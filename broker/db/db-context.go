package dbContext

import (
	"context"
	queries "github.com/indexdata/crosslink/broker/db/generated"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
)

var DbPool *pgxpool.Pool

func SetDbPool(pool *pgxpool.Pool) {
	DbPool = pool
}

func GetDbQueries() *queries.Queries {
	con, err := DbPool.Acquire(context.Background())
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	return queries.New(con)
}
