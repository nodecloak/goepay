package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
)

type MembershipPurchase struct {
	ID, MerchantID, GroupID, Price int64
	GroupName, ExternalNo          string
	DurationDays                   int
	ExpiresAt                      time.Time
	SourceVersion                  int
	Actor                          string
	CreatedAt                      time.Time
}

func scanMembership(row pgx.Row) (MembershipPurchase, error) {
	var m MembershipPurchase
	e := row.Scan(&m.ID, &m.MerchantID, &m.GroupID, &m.Price, &m.GroupName, &m.ExternalNo, &m.DurationDays, &m.ExpiresAt, &m.SourceVersion, &m.Actor, &m.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return m, e
}

const membershipColumns = "id,merchant_id,group_id,price,group_name,external_no,duration_days,expires_at,source_version,actor,created_at"

func (s *Store) PurchaseMembership(ctx context.Context, merchantID, groupID int64, externalNo, actor string) (MembershipPurchase, error) {
	if merchantID <= 0 || groupID <= 0 || externalNo == "" || len(externalNo) > 128 || actor == "" {
		return MembershipPurchase{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return MembershipPurchase{}, e
	}
	defer tx.Rollback(ctx)
	if e = s.membershipLockParties(ctx, tx, merchantID); e != nil {
		return MembershipPurchase{}, e
	}
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", merchantID).Scan(&enabled); e != nil {
		return MembershipPurchase{}, e
	}
	if !enabled {
		return MembershipPurchase{}, ErrState
	}
	old, e := scanMembership(tx.QueryRow(ctx, "SELECT "+membershipColumns+" FROM membership_purchases WHERE merchant_id=$1 AND external_no=$2", merchantID, externalNo))
	if e == nil {
		if old.GroupID != groupID {
			return MembershipPurchase{}, ErrConflict
		}
		if e = s.verifyMembershipPurchase(ctx, tx, old); e != nil {
			return MembershipPurchase{}, e
		}
		return old, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return MembershipPurchase{}, e
	}
	var name, priceRaw, daysRaw string
	if e = tx.QueryRow(ctx, "SELECT body->>'name',COALESCE(body->>'price_cents','0'),COALESCE(body->>'duration_days','30') FROM backoffice_records WHERE resource='groups' AND id=$1 AND body->>'enabled'='true' FOR SHARE", groupID).Scan(&name, &priceRaw, &daysRaw); e != nil {
		return MembershipPurchase{}, ErrNotFound
	}
	price, e := strconv.ParseInt(priceRaw, 10, 64)
	if e != nil || price < 0 {
		return MembershipPurchase{}, ErrInvalid
	}
	days, e := strconv.Atoi(daysRaw)
	if e != nil || days < 1 || days > 3660 {
		return MembershipPurchase{}, ErrInvalid
	}
	quote, e := s.membershipReferralDecision(ctx, tx, merchantID, groupID, externalNo)
	if e != nil || quote.Price != price {
		if e == nil {
			e = ErrConflict
		}
		return MembershipPurchase{}, e
	}
	var purchaseID int64
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('membership_purchases','id'))").Scan(&purchaseID); e != nil {
		return MembershipPurchase{}, e
	}
	quote, e = s.prepareMembershipReferralFunding(ctx, tx, purchaseID, quote)
	if e != nil {
		return MembershipPurchase{}, e
	}
	current, e := s.membershipReferralDecision(ctx, tx, merchantID, groupID, externalNo)
	if e != nil || current.ProofFingerprint != quote.ProofFingerprint {
		if e == nil {
			e = ErrConflict
		}
		return MembershipPurchase{}, e
	}
	if e = requireFundsAction(ctx, tx, "membership", fmt.Sprintf("%d:%s", merchantID, externalNo)); e != nil {
		return MembershipPurchase{}, e
	}
	current, e = s.membershipReferralDecision(ctx, tx, merchantID, groupID, externalNo)
	if e != nil || current.ProofFingerprint != quote.ProofFingerprint {
		if e == nil {
			e = ErrConflict
		}
		return MembershipPurchase{}, e
	}
	var at time.Time
	if e = tx.QueryRow(ctx, "SELECT pg_catalog.clock_timestamp()").Scan(&at); e != nil {
		return MembershipPurchase{}, e
	}
	var expiration time.Time
	e = tx.QueryRow(ctx, "SELECT CASE WHEN group_id=$2 AND expires_at>$3 THEN expires_at ELSE $3 END FROM memberships WHERE merchant_id=$1", merchantID, groupID, at).Scan(&expiration)
	if errors.Is(e, pgx.ErrNoRows) {
		expiration = at
	} else if e != nil {
		return MembershipPurchase{}, e
	}
	expiration = expiration.Add(time.Duration(days) * 24 * time.Hour)
	m, e := scanMembership(tx.QueryRow(ctx, "INSERT INTO membership_purchases(id,merchant_id,group_id,price,group_name,external_no,duration_days,expires_at,actor,source_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,1) RETURNING "+membershipColumns, purchaseID, merchantID, groupID, price, name, externalNo, days, expiration, actor))
	if e != nil {
		return m, e
	}
	if e = s.fundMembershipReferral(ctx, tx, m, quote); e != nil {
		return m, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES($1,$2,$3) ON CONFLICT(merchant_id) DO UPDATE SET group_id=$2,expires_at=$3", merchantID, groupID, expiration); e != nil {
		return m, e
	}
	if _, e = tx.Exec(ctx, "UPDATE merchants SET config_revision=config_revision+1 WHERE id=$1", merchantID); e != nil {
		return m, e
	}
	return m, tx.Commit(ctx)
}

type DepositAction struct {
	ID, MerchantID, Amount     int64
	Action, ExternalNo, Status string
	CreatedAt                  time.Time
}

const depositColumns = "id,merchant_id,amount,action,external_no,status,created_at"

func scanDeposit(row pgx.Row) (DepositAction, error) {
	var d DepositAction
	e := row.Scan(&d.ID, &d.MerchantID, &d.Amount, &d.Action, &d.ExternalNo, &d.Status, &d.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return d, e
}
func (s *Store) Deposit(ctx context.Context, merchantID int64, action, externalNo string, amount int64, actor string) (DepositAction, error) {
	if merchantID <= 0 || (action != "hold" && action != "release") || amount <= 0 || externalNo == "" || len(externalNo) > 128 || actor == "" {
		return DepositAction{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return DepositAction{}, e
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR UPDATE", merchantID).Scan(&enabled); e != nil {
		return DepositAction{}, e
	}
	if !enabled {
		return DepositAction{}, ErrState
	}
	old, e := scanDeposit(tx.QueryRow(ctx, "SELECT "+depositColumns+" FROM deposit_actions WHERE merchant_id=$1 AND external_no=$2", merchantID, externalNo))
	if e == nil {
		if old.Amount != amount || old.Action != action {
			return DepositAction{}, ErrConflict
		}
		return old, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return DepositAction{}, e
	}
	if e = requireFundsAction(ctx, tx, "deposit", fmt.Sprintf("%d:%s", merchantID, externalNo)); e != nil {
		return DepositAction{}, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO accounts(owner_id,kind) VALUES($1,'deposit'),($1,'deposit_reserved') ON CONFLICT DO NOTHING", merchantID); e != nil {
		return DepositAction{}, e
	}
	status := "held"
	from, to := "available", "deposit"
	if action == "release" {
		status = "pending"
		from, to = "deposit", "deposit_reserved"
	}
	d, e := scanDeposit(tx.QueryRow(ctx, "INSERT INTO deposit_actions(merchant_id,action,external_no,amount,status,created_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING "+depositColumns, merchantID, action, externalNo, amount, status, actor))
	if e != nil {
		return d, e
	}
	if e = post(ctx, tx, fmt.Sprintf("deposit:%d:request", d.ID), "deposit "+action, merchantID, from, merchantID, to, amount); e != nil {
		return d, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO deposit_action_events(action_id,status,actor) VALUES($1,$2,$3)", d.ID, status, actor); e != nil {
		return d, e
	}
	return d, tx.Commit(ctx)
}
func (s *Store) ReviewDeposit(ctx context.Context, id int64, approve bool, actor string) (DepositAction, error) {
	if id <= 0 || actor == "" {
		return DepositAction{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return DepositAction{}, e
	}
	defer tx.Rollback(ctx)
	d, e := scanDeposit(tx.QueryRow(ctx, "SELECT "+depositColumns+" FROM deposit_actions WHERE id=$1 FOR UPDATE", id))
	if e != nil {
		return d, e
	}
	status, to := "rejected", "deposit"
	if approve {
		status, to = "approved", "available"
	}
	if d.Status == status {
		return d, nil
	}
	if d.Status != "pending" || d.Action != "release" {
		return d, ErrState
	}
	if e = requireFundsAction(ctx, tx, "deposit-review", fmt.Sprintf("%d:%t", id, approve)); e != nil {
		return d, e
	}
	if e = post(ctx, tx, fmt.Sprintf("deposit:%d:review", id), "deposit release "+status, d.MerchantID, "deposit_reserved", d.MerchantID, to, d.Amount); e != nil {
		return d, e
	}
	if _, e = tx.Exec(ctx, "UPDATE deposit_actions SET status=$2,reviewed_by=$3,reviewed_at=now() WHERE id=$1", id, status, actor); e != nil {
		return d, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO deposit_action_events(action_id,status,actor) VALUES($1,$2,$3)", id, status, actor); e != nil {
		return d, e
	}
	d.Status = status
	return d, tx.Commit(ctx)
}
func (s *Store) DepositBalance(ctx context.Context, id int64) (held, pending int64, e error) {
	e = s.pool.QueryRow(ctx, "SELECT COALESCE(sum(balance) FILTER(WHERE kind='deposit'),0),COALESCE(sum(balance) FILTER(WHERE kind='deposit_reserved'),0) FROM accounts WHERE owner_id=$1", id).Scan(&held, &pending)
	return
}
