package postgres

import (
	"context"
	"errors"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/logger"
	"github.com/lib/pq"
	"github.com/lib/pq/pqerror"
	"go.uber.org/zap"
)

// PostgreSQL error codes
// See: https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	pgUniqueViolation      pqerror.Code = "23505"
	pgCheckViolation       pqerror.Code = "23514"
	pgNotNullViolation     pqerror.Code = "23502"
	pgForeignKeyViolation  pqerror.Code = "23503"
	pgSerializationFailure pqerror.Code = "40001"
	pgDeadlockDetected     pqerror.Code = "40P01"
	pgExclusionViolation   pqerror.Code = "23P01"
)

// Error maps PostgreSQL-specific errors to domain errors.
// Any unmapped error is returned as-is so the transactor can still rollback.
// If the database changes (e.g. MySQL), this package must be replaced or adapted.
func Error(ctx context.Context, err error) error {
	var pgErr *pq.Error
	if !errors.As(err, &pgErr) {
		logger.Of(ctx).Warn("database.error",
			zap.Error(err),
		)
		return err
	}

	switch pgErr.Code {
	case pgUniqueViolation:
		return app.ErrDataConflict

	case pgCheckViolation, pgForeignKeyViolation, pgNotNullViolation, pgExclusionViolation:
		return app.ErrDataViolation

	case pgSerializationFailure, pgDeadlockDetected:
		logger.Of(ctx).Warn("database.conflict",
			zap.String("code", string(pgErr.Code)),
			zap.String("message", pgErr.Message),
		)
		return app.ErrInfraConflict

	default:
		logger.Of(ctx).Warn("database.unhandled_error",
			zap.String("code", string(pgErr.Code)),
			zap.String("message", pgErr.Message),
		)
		return err
	}
}
