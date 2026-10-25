package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"slices"
)

// The selected set becomes one durable business intent with the first claim.
// A later item uses this immutable proof, never an in-memory password unlock.
type batchExecution struct {
	no  string
	ids []int64
}

func newBatchExecution(ctx context.Context, b TransferBatch, limit int) (*batchExecution, error) {
	if guard, _ := ctx.Value(fundsActionGuardKey{}).(FundsActionGuard); guard == nil {
		return nil, nil
	}
	var ids []int64
	for _, item := range b.Items {
		if item.SendState == "unsent" {
			ids = append(ids, item.Transfer.ID)
			if len(ids) == limit {
				break
			}
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return &batchExecution{no: hex.EncodeToString(nonce), ids: ids}, nil
}

func (x *batchExecution) claim(ctx context.Context, tx pgx.Tx, mid, batchID, transferID int64, actor TransferBatchActor) error {
	if x == nil {
		return nil
	}
	if !slices.Contains(x.ids, transferID) {
		return ErrConflict
	}
	var ids []int64
	var batch, merchant, actorID int64
	var role, name string
	err := tx.QueryRow(ctx, "SELECT batch_id,merchant_id,actor_role,actor_id,actor_name,transfer_ids FROM transfer_batch_executions WHERE execution_no=$1", x.no).Scan(&batch, &merchant, &role, &actorID, &name, &ids)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = requireFundsAction(ctx, tx, "batch-execute", fmt.Sprintf("%d:%s", batchID, x.no)); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "INSERT INTO transfer_batch_executions(execution_no,batch_id,merchant_id,actor_role,actor_id,actor_name,transfer_ids) VALUES($1,$2,$3,$4,$5,$6,$7)", x.no, batchID, mid, actor.Role, actor.ID, actor.Name, x.ids)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if batch != batchID || merchant != mid || role != actor.Role || actorID != actor.ID || name != actor.Name || !slices.Equal(ids, x.ids) {
		return ErrConflict
	}
	_, err = tx.Exec(ctx, "INSERT INTO transfer_batch_execution_claims(transfer_id,execution_no) VALUES($1,$2)", transferID, x.no)
	return err
}
