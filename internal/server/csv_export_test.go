package server

import (
	"context"
	"encoding/csv"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCSVExportRowLimitDoesNotReturnPartialFile(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	pool, e := pgxpool.New(context.Background(), dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := &Server{Pool: pool}
	r := httptest.NewRequest("GET", "/console/v1/merchant/orders?format=csv", nil)
	w := httptest.NewRecorder()
	s.exportCSV(w, r, identity.Session{}, "orders", "SELECT generate_series(1,50001) AS id", nil)
	if w.Code != 422 || strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") || w.Header().Get("Content-Disposition") != "" {
		t.Fatalf("partial oversized CSV returned: %d", w.Code)
	}
}

func TestCSVExportBusyIsBounded(t *testing.T) {
	csvExportSlots <- struct{}{}
	csvExportSlots <- struct{}{}
	defer func() { <-csvExportSlots; <-csvExportSlots }()
	w := httptest.NewRecorder()
	s := &Server{}
	s.exportCSV(w, httptest.NewRequest("GET", "/", nil), identity.Session{}, "orders", "", nil)
	if w.Code != 429 || w.Header().Get("Retry-After") != "5" {
		t.Fatalf("export concurrency not bounded: %d", w.Code)
	}
}

func TestCSVExportCellPreventsSpreadsheetFormula(t *testing.T) {
	for _, input := range []string{"=1+1", "+cmd", "-1+1", "@SUM(1)", " \t=1", "\r\n@x", "\x00-1", "\u2003+1"} {
		if got := csvExportCell(input); got != "'"+input {
			t.Fatalf("formula not neutralized %q", got)
		}
	}
	for _, input := range []string{"ordinary", "12.34", "逗号,引号\"\n新行", "", "\twords"} {
		if got := csvExportCell(input); got != input {
			t.Fatalf("text changed %q", got)
		}
	}
	if got := csvExportCell(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); got != "2026-01-01T08:00:00+08:00" {
		t.Fatal(got)
	}
	if csvExportCell(nil) != "" || csvExportCell(true) != "true" {
		t.Fatal("scalar formatting")
	}
}

func TestCSVExportBoundedBufferNeverTruncatesSuccessfully(t *testing.T) {
	b := &csvBoundedBuffer{}
	writer := csv.NewWriter(b)
	if e := writer.Write([]string{strings.Repeat("x", csvExportMaxBytes)}); e != nil && !errors.Is(e, errCSVExportTooLarge) {
		t.Fatal(e)
	}
	writer.Flush()
	if !errors.Is(writer.Error(), errCSVExportTooLarge) {
		t.Fatalf("oversize silently accepted: %v", writer.Error())
	}
	if b.Len() > csvExportMaxBytes {
		t.Fatal("memory limit exceeded")
	}
}
