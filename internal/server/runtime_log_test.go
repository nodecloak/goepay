package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type runtimePrivateError struct{ state string }

func (e runtimePrivateError) Error() string {
	return "password=runtime-sentinel SELECT private_credentials FROM private_table"
}
func (e runtimePrivateError) SQLState() string { return e.state }

func TestRuntimeFailureNeverPersistsRawErrorOrSQL(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{errors.New("password=runtime-sentinel"), "INTERNAL"},
		{runtimePrivateError{"42P01"}, "42P01"},
		{fmt.Errorf("secret wrapper: %w", runtimePrivateError{"23505"}), "23505"},
		{runtimePrivateError{"SELECT secret"}, "INTERNAL"},
		{context.DeadlineExceeded, "TIMEOUT"},
		{context.Canceled, "CANCELED"},
	} {
		level, code, content := safeRuntimeFailure(tc.err)
		if code != tc.code || level == "" || content == "" || strings.Contains(content, "runtime-sentinel") || strings.Contains(content, "SELECT") || strings.Contains(content, "private_") {
			t.Fatalf("unsafe runtime projection: %q %q %q", level, code, content)
		}
	}
}
