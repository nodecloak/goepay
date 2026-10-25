package payment

import (
	"context"
	"fmt"
	"testing"
)

// A remote operation result and its audit are insufficient on their own: the
// global barrier and owning receiver must resolve in the same transaction.
func TestChannelSharingRelationReferencesDatabaseRejectsDetachedRemoteResult(t *testing.T) {
	for _, action := range []string{"bind", "unbind"} {
		for _, uncertain := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_unknown_%t", action, uncertain), func(t *testing.T) {
				s, g, calls, lost := sharingReceiverFlowFixture(t)
				ctx := context.Background()
				r, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "state-guard-original", channelSharingReceiverInput, channelSharingActor)
				if e != nil {
					t.Fatal(e)
				}
				if action == "unbind" {
					if _, e = g.BindChannelSharingReceiver(ctx, 751, r.ID, "state-guard-bind", channelSharingActor); e != nil {
						t.Fatal(e)
					}
				}
				var op ChannelSharingReceiverOperation
				pending := "binding"
				if action == "unbind" {
					pending = "unbinding"
				}
				originalState := "claimed"
				if uncertain {
					lost.Store(true)
					_, e = g.receiverOperation(ctx, 751, r.ID, "state-guard-original-send", action, channelSharingActor)
					if e == nil {
						t.Fatal("local fixture did not produce an uncertain result")
					}
					current, e := s.ChannelSharingReceiver(ctx, 751, r.ID, channelSharingActor)
					if e != nil || current.Operation == nil {
						t.Fatal(current, e)
					}
					op = *current.Operation
					pending = "unknown"
					originalState = "unknown"
				} else {
					_, binding, e := g.sharingChannel(751, 7)
					if e != nil {
						t.Fatal(e)
					}
					var owner bool
					op, _, owner, e = s.claimSharingReceiverOperation(ctx, 751, r.ID, "state-guard-original-claim", action, binding, channelSharingActor)
					if e != nil || !owner {
						t.Fatal(op, owner, e)
					}
				}
				before := calls.Load()
				targets := []string{"failed", "unknown", "succeeded"}
				if uncertain {
					targets = []string{"failed"}
				}
				for _, target := range targets {
					if !t.Run(target, func(t *testing.T) {
						for _, updateGlobal := range []bool{false, true} {
							if uncertain && updateGlobal {
								continue
							}
							tx, e := s.pool.Begin(ctx)
							if e != nil {
								t.Fatal(e)
							}
							defer tx.Rollback(ctx)
							if _, e = tx.Exec(ctx, "UPDATE channel_sharing_receiver_operations SET state=$2 WHERE id=$1", op.ID, target); e != nil {
								t.Fatal(e)
							}
							if e = sharingConfigurationAudit(ctx, tx, channelSharingActor, "channel-sharing-receiver-operations", op.ID, action, target); e != nil {
								t.Fatal(e)
							}
							if updateGlobal {
								// Also reject changing the registry result while leaving its
								// actual operation owner's receiver barrier unresolved.
								next := "unknown"
								if action == "bind" {
									var bound any
									if target == "failed" {
										next = "unbound"
									}
									if target == "succeeded" {
										next = "bound"
										bound = op.ID
									}
									_, e = tx.Exec(ctx, "UPDATE channel_sharing_relations SET state=$2,bound_operation_id=$3 WHERE id=$1", op.RelationID, next, bound)
								} else {
									if target == "failed" {
										next = "bound"
									}
									if target == "succeeded" {
										next = "unbound"
									}
									_, e = tx.Exec(ctx, "UPDATE channel_sharing_relations SET state=$2 WHERE id=$1", op.RelationID, next)
								}
								if e != nil {
									t.Fatal(e)
								}
							}
							if e = tx.Commit(ctx); e == nil {
								t.Fatalf("database committed detached result, registry updated=%t; original receiver unresolved", updateGlobal)
							}
							var global, local, state string
							e = s.pool.QueryRow(ctx, "SELECT g.state,r.state,op.state FROM channel_sharing_relations g JOIN channel_sharing_receiver_operations op ON op.id=g.last_operation_id JOIN channel_sharing_receivers r ON r.id=op.receiver_id WHERE g.id=$1", op.RelationID).Scan(&global, &local, &state)
							if e != nil || global != pending || local != pending || state != originalState || calls.Load() != before {
								t.Fatal("rejected mutation changed the original barrier", global, local, state, calls.Load(), e)
							}
						}
					}) {
						// A broken implementation may have committed the mutation;
						// stop this fixture rather than testing its corrupted state.
						break
					}
				}
			})
		}
	}
}
