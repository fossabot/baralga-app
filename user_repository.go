package main

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/pkg/errors"
)

var ErrUserNotFound = errors.New("user not found")

type User struct {
	ID             uuid.UUID
	Name           string
	Username       string
	EMail          string
	Password       string
	OrganizationID uuid.UUID
}

type Organization struct {
	ID    uuid.UUID
	Title string
}

type UserRepository interface {
	ConfirmUser(ctx context.Context, userID uuid.UUID) error
	FindUserIDByConfirmationID(ctx context.Context, confirmationID string) (uuid.UUID, error)
	InsertUserWithConfirmationID(ctx context.Context, user *User, confirmationID uuid.UUID) (*User, error)
	FindUserByUsername(ctx context.Context, username string) (*User, error)
	FindRolesByUserID(ctx context.Context, organizationID, userID uuid.UUID) ([]string, error)
}

// DbUserRepository is a SQL database repository for users
type DbUserRepository struct {
	connPool *pgxpool.Pool
}

var _ UserRepository = (*DbUserRepository)(nil)

// NewDbUserRepository creates a new SQL database repository for users
func NewDbUserRepository(connPool *pgxpool.Pool) *DbUserRepository {
	return &DbUserRepository{
		connPool: connPool,
	}
}

func (r *DbUserRepository) insertConfirmation(ctx context.Context, tx pgx.Tx, user *User, confirmationID uuid.UUID) (uuid.UUID, error) {
	_, err := tx.Exec(
		ctx,
		`INSERT INTO user_confirmations 
		   (user_confirmation_id, user_id) 
		 VALUES 
		   ($1, $2)`,
		confirmationID,
		user.ID,
	)
	return confirmationID, err
}

func (r *DbUserRepository) InsertUserWithConfirmationID(ctx context.Context, user *User, confirmationID uuid.UUID) (*User, error) {
	tx := ctx.Value(contextKeyTx).(pgx.Tx)

	_, err := tx.Exec(
		ctx,
		`INSERT INTO users 
		   (user_id, username, email, name, password, enabled, org_id) 
		 VALUES 
		   ($1, $2, $3, $4, $5, $6, $7)`,
		user.ID,
		user.Username,
		user.EMail,
		user.Name,
		user.Password,
		0,
		user.OrganizationID,
	)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(
		ctx,
		`INSERT INTO roles 
		   (user_id, role, org_id) 
		 VALUES 
		   ($1, 'ROLE_ADMIN', $2)`,
		user.ID,
		user.OrganizationID,
	)
	if err != nil {
		return nil, err
	}

	_, err = r.insertConfirmation(ctx, tx, user, confirmationID)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (r *DbUserRepository) FindUserIDByConfirmationID(ctx context.Context, confirmationID string) (uuid.UUID, error) {
	row := r.connPool.QueryRow(
		ctx,
		`SELECT user_id 
		 FROM user_confirmations 
		 WHERE user_confirmation_id = $1`, confirmationID,
	)

	var (
		userID string
	)

	err := row.Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrUserNotFound
		}

		return uuid.Nil, err
	}

	return uuid.MustParse(userID), nil
}

func (r *DbUserRepository) ConfirmUser(ctx context.Context, userID uuid.UUID) error {
	tx := ctx.Value(contextKeyTx).(pgx.Tx)

	_, err := tx.Exec(
		ctx,
		`DELETE FROM user_confirmations 
		 WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(
		ctx,
		`UPDATE users
		 SET enabled = 1 
		 WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return err
	}

	return nil
}

func (r *DbUserRepository) FindUserByUsername(ctx context.Context, username string) (*User, error) {
	row := r.connPool.QueryRow(
		ctx,
		`SELECT user_id, password, org_id 
		 FROM users 
		 WHERE username = $1 AND enabled = 1`, username,
	)

	var (
		id             string
		password       string
		organizationID string
	)

	err := row.Scan(&id, &password, &organizationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}

		return nil, err
	}

	user := &User{
		ID:             uuid.MustParse(id),
		Username:       username,
		Password:       password,
		OrganizationID: uuid.MustParse(organizationID),
	}
	return user, nil
}

func (r *DbUserRepository) FindRolesByUserID(ctx context.Context, organizationID, userID uuid.UUID) ([]string, error) {
	rows, err := r.connPool.Query(
		ctx,
		`SELECT role 
		 FROM roles 
		 WHERE user_id = $1 AND org_id = $2`, userID, organizationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var role string

		err = rows.Scan(&role)
		if err != nil {
			return nil, err
		}

		roles = append(roles, role)
	}

	return roles, nil
}
