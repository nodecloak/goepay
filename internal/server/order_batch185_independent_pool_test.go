package server
import("context";"testing";"time";"errors";"strconv";"goepay/internal/payment";"goepay/internal/identity";"github.com/jackc/pgx/v5/pgxpool")
func TestOrderBatch185IndependentSingleConnectionPoolDoesNotSelfDeadlock(t *testing.T){
 f,factory,_:=subchannel168BProductLocal(t);ctx:=context.Background()
 if out:=f.pay(t,"pool-single185","1.00",7);out["code"]!=float64(1){t.Fatal(out)}
 o,e:=f.s.Store.Order(ctx,1,"pool-single185");if e!=nil{t.Fatal(e)}
 v:=batch185Prepared(t,f,o.TradeNo,"pool-single185");id,e:=strconv.ParseInt(v["id"].(string),10,64);if e!=nil{t.Fatal(e)}
 cfg:=f.p.Config();cfg.MaxConns=1;cfg.MinConns=0;p,e:=pgxpool.NewWithConfig(ctx,cfg);if e!=nil{t.Fatal(e)};defer p.Close()
 s,e:=payment.NewWithKey(p,make([]byte,32));if e!=nil{t.Fatal(e)};g:=*f.s.ProviderFlow;g.Store=s
 u:=identity.User{ID:168,Role:"admin",Username:"admin168",AdminClass:"super"};if e=f.p.QueryRow(ctx,"SELECT auth_version FROM console_users WHERE username='admin168'").Scan(&u.AuthVersion);e!=nil{t.Fatal(e)}
 timed,cancel:=context.WithTimeout(ctx,1500*time.Millisecond);defer cancel()
 _,e=g.ExecuteOrderBusinessBatchQuery(timed,payment.OrderBusinessBatchActor{User:u,Token:f.token},id,1)
 t.Logf("one pool connection, no competing request, query count=%d, error=%v",factory.providers["products-v1"].queries.Load(),e)
 if errors.Is(e,context.DeadlineExceeded){t.Fatal("executor held the only pool connection for advisory lock and could not begin its claim transaction")};if e!=nil{t.Fatal(e)}
}
