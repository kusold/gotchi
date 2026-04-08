package db

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
)

type mockTracer struct {
	startCalled bool
	endCalled   bool
}

func (m *mockTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	m.startCalled = true
	return ctx
}

func (m *mockTracer) TraceQueryEnd(_ context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	m.endCalled = true
}

func TestMultiTracer_DelegatesToAll(t *testing.T) {
	t1 := &mockTracer{}
	t2 := &mockTracer{}

	mt := &multiTracer{tracers: []pgx.QueryTracer{t1, t2}}

	mt.TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{})

	assert.True(t, t1.endCalled, "first tracer end should be called")
	assert.True(t, t2.endCalled, "second tracer end should be called")
}

func TestMultiTracer_TraceQueryStart(t *testing.T) {
	t1 := &mockTracer{}
	t2 := &mockTracer{}

	mt := &multiTracer{tracers: []pgx.QueryTracer{t1, t2}}

	ctx := mt.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{})

	assert.NotNil(t, ctx)
	assert.True(t, t1.startCalled)
	assert.True(t, t2.startCalled)
}

func TestMultiTracer_EmptyTracers(t *testing.T) {
	mt := &multiTracer{tracers: nil}

	assert.NotPanics(t, func() {
		mt.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{})
		mt.TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{})
	})
}

func TestSetupTracing_SingleTracer(t *testing.T) {
	cfg := pgxpoolConfig(t)
	result := setupTracing(cfg, Config{EnableSlogTracing: true})
	assert.NotNil(t, result.ConnConfig.Tracer)
}

func TestSetupTracing_MultipleTracers(t *testing.T) {
	cfg := pgxpoolConfig(t)
	result := setupTracing(cfg, Config{EnableSlogTracing: true, OTELTracing: true})
	_, ok := result.ConnConfig.Tracer.(*multiTracer)
	assert.True(t, ok, "should use multiTracer when both tracing types are enabled")
}

func TestSetupTracing_NoTracing(t *testing.T) {
	cfg := pgxpoolConfig(t)
	result := setupTracing(cfg, Config{})
	assert.Nil(t, result.ConnConfig.Tracer)
}

func TestSetupTracing_OTELOnly(t *testing.T) {
	cfg := pgxpoolConfig(t)
	result := setupTracing(cfg, Config{OTELTracing: true})
	assert.NotNil(t, result.ConnConfig.Tracer)
	_, ok := result.ConnConfig.Tracer.(*multiTracer)
	assert.False(t, ok, "should not use multiTracer for single tracer")
}

func pgxpoolConfig(t *testing.T) *pgxpool.Config {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://user:pass@localhost:5432/testdb")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
