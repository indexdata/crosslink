package repository

import (
	"context"
	queries "github.com/indexdata/crosslink/broker/db/generated"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
)

type Repository interface {
	CreateIllTransaction(ctx context.Context, params queries.CreateIllTransactionParams) (queries.CreateIllTransactionRow, error)
}

type PostgresRepository struct {
	DbPool *pgxpool.Pool
}

func (r *PostgresRepository) CreateIllTransaction(ctx context.Context, params queries.CreateIllTransactionParams) (queries.CreateIllTransactionRow, error) {
	return GetDbQueries(r.DbPool).CreateIllTransaction(ctx, params)
}

func GetDbQueries(dbPool *pgxpool.Pool) *queries.Queries {
	con, err := dbPool.Acquire(context.Background())
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	return queries.New(con)
}
