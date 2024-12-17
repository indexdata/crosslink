package repository

import (
	"context"
	queries "github.com/indexdata/crosslink/broker/db/generated"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
)

type Repository interface {
	CreateIllTransaction(ctx context.Context, params queries.CreateIllTransactionParams) (queries.CreateIllTransactionRow, error)
	CreateEvent(ctx context.Context, params queries.CreateEventParams) (queries.CreateEventRow, error)
	GetIllTransactionByRequesterRequestId(ctx context.Context, requesterRequestID pgtype.Text) (queries.GetIllTransactionByRequesterRequestIdRow, error)
}

type PostgresRepository struct {
	DbPool *pgxpool.Pool
}

func (r *PostgresRepository) CreateIllTransaction(ctx context.Context, params queries.CreateIllTransactionParams) (queries.CreateIllTransactionRow, error) {
	return GetDbQueries(r.DbPool).CreateIllTransaction(ctx, params)
}

func (r *PostgresRepository) CreateEvent(ctx context.Context, params queries.CreateEventParams) (queries.CreateEventRow, error) {
	return GetDbQueries(r.DbPool).CreateEvent(ctx, params)
}

func (r *PostgresRepository) GetIllTransactionByRequesterRequestId(ctx context.Context, requesterRequestID pgtype.Text) (queries.GetIllTransactionByRequesterRequestIdRow, error) {
	return GetDbQueries(r.DbPool).GetIllTransactionByRequesterRequestId(ctx, requesterRequestID)
}

func GetDbQueries(dbPool *pgxpool.Pool) *queries.Queries {
	con, err := dbPool.Acquire(context.Background())
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	return queries.New(con)
}
