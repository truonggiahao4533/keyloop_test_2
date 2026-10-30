// Package postgres implements the domain repository ports on PostgreSQL.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/haotruong/inventory-dashboard/db/gen"
	"github.com/haotruong/inventory-dashboard/internal/domain"
)

// DefaultTimeout bounds every repository call.
const DefaultTimeout = 3 * time.Second

// PoolConfig configures the connection pool.
type PoolConfig struct {
	URL      string
	MaxConns int32
	Tracer   pgx.QueryTracer // optional
}

// NewPool opens a pgx pool.
func NewPool(ctx context.Context, pc PoolConfig) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(pc.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if pc.MaxConns > 0 {
		cfg.MaxConns = pc.MaxConns
	}
	cfg.MaxConnIdleTime = 5 * time.Minute
	// The list query uses "$n IS NULL OR ..." optional filters. A generic
	// cached plan cannot fold those away and may stop using the index, so
	// always plan with the actual parameter values.
	cfg.ConnConfig.RuntimeParams["plan_cache_mode"] = "force_custom_plan"
	if pc.Tracer != nil {
		cfg.ConnConfig.Tracer = pc.Tracer
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	return pool, nil
}

type base struct {
	pool    *pgxpool.Pool
	q       *gen.Queries
	timeout time.Duration
}

func (b base) ctx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, b.timeout)
}

// Repos bundles the repository implementations.
type Repos struct {
	Vehicles *VehicleRepo
	Actions  *ActionRepo
	Settings *SettingsRepo
}

// NewRepos builds repositories on pool. timeout <= 0 uses DefaultTimeout.
func NewRepos(pool *pgxpool.Pool, timeout time.Duration) Repos {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	b := base{pool: pool, q: gen.New(pool), timeout: timeout}
	return Repos{&VehicleRepo{b}, &ActionRepo{b}, &SettingsRepo{b}}
}

// VehicleRepo implements domain.VehicleRepo.
type VehicleRepo struct{ base }

var _ domain.VehicleRepo = (*VehicleRepo)(nil)

// List implements domain.VehicleRepo.
func (r *VehicleRepo) List(ctx context.Context, f domain.VehicleFilter, c *domain.Cursor, limit int) ([]domain.VehicleRow, *domain.Cursor, error) {
	ctx, cancel := r.ctx(ctx)
	defer cancel()
	p := gen.ListVehiclesParams{
		Make:           optText(f.Make),
		Model:          optText(f.Model),
		ReceivedBefore: optDate(f.ReceivedBefore),
		ReceivedAfter:  optDate(f.ReceivedAfter),
		Lim:            int32(limit + 1), // one extra row tells us if there is a next page
	}
	if c != nil {
		p.CurDate = pgtype.Date{Time: c.Date, Valid: true}
		p.CurID = uuid.NullUUID{UUID: c.ID, Valid: true}
	}
	rows, err := r.q.ListVehicles(ctx, p)
	if err != nil {
		return nil, nil, fmt.Errorf("list vehicles: %w", err)
	}
	var next *domain.Cursor
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		next = &domain.Cursor{Date: last.ReceivedAt.Time, ID: last.ID}
	}
	out := make([]domain.VehicleRow, len(rows))
	for i, row := range rows {
		if out[i], err = toVehicleRow(row); err != nil {
			return nil, nil, err
		}
	}
	return out, next, nil
}

// Summary implements domain.VehicleRepo. All counts come from one snapshot.
func (r *VehicleRepo) Summary(ctx context.Context, today time.Time, threshold int) (domain.Summary, error) {
	ctx, cancel := r.ctx(ctx)
	defer cancel()
	cutoff := pgtype.Date{Time: domain.AgingCutoff(today, threshold), Valid: true}
	var s domain.Summary
	err := withTx(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(q *gen.Queries) error {
		total, err := q.CountInStock(ctx)
		if err != nil {
			return err
		}
		s.TotalInStock = int(total)
		byMake, err := q.AgingByMake(ctx, cutoff)
		if err != nil {
			return err
		}
		s.AgingByMake = make([]domain.MakeCount, len(byMake))
		for i, m := range byMake {
			s.AgingByMake[i] = domain.MakeCount{Make: m.Make, Count: int(m.Count)}
			s.AgingCount += int(m.Count)
		}
		byAction, err := q.AgingByAction(ctx, cutoff)
		if err != nil {
			return err
		}
		s.AgingByAction = make([]domain.ActionCount, len(byAction))
		for i, a := range byAction {
			s.AgingByAction[i] = domain.ActionCount{Action: fromNullAction(a.CurrentActionType), Count: int(a.Count)}
		}
		return nil
	})
	if err != nil {
		return domain.Summary{}, fmt.Errorf("summary: %w", err)
	}
	return s, nil
}

// Upsert implements domain.VehicleRepo. Rows whose fields are unchanged are
// neither inserted nor updated.
func (r *VehicleRepo) Upsert(ctx context.Context, vs []domain.Vehicle) (domain.UpsertResult, error) {
	ctx, cancel := r.ctx(ctx)
	defer cancel()
	p := gen.UpsertVehiclesParams{
		Ids:         make([]uuid.UUID, len(vs)),
		Vins:        make([]string, len(vs)),
		Makes:       make([]string, len(vs)),
		Models:      make([]string, len(vs)),
		Years:       make([]int32, len(vs)),
		Prices:      make([]pgtype.Numeric, len(vs)),
		ReceivedAts: make([]pgtype.Date, len(vs)),
		Statuses:    make([]string, len(vs)),
	}
	for i, v := range vs {
		id := v.ID
		if id == uuid.Nil {
			id = uuid.Must(uuid.NewV7())
		}
		p.Ids[i] = id
		p.Vins[i] = v.VIN
		p.Makes[i] = v.Make
		p.Models[i] = v.Model
		p.Years[i] = int32(v.Year)
		p.Prices[i] = centsToNumeric(v.PriceCents)
		p.ReceivedAts[i] = pgtype.Date{Time: domain.DateOf(v.ReceivedAt), Valid: true}
		p.Statuses[i] = string(v.Status)
	}
	inserted, err := r.q.UpsertVehicles(ctx, p)
	if err != nil {
		return domain.UpsertResult{}, fmt.Errorf("upsert vehicles: %w", err)
	}
	var res domain.UpsertResult
	for _, ins := range inserted {
		if ins {
			res.Inserted++
		} else {
			res.Updated++
		}
	}
	return res, nil
}

// ActionRepo implements domain.ActionRepo.
type ActionRepo struct{ base }

var _ domain.ActionRepo = (*ActionRepo)(nil)

// storedAction is the idempotency record body for a logged action.
type storedAction struct {
	ID        uuid.UUID         `json:"id"`
	VehicleID uuid.UUID         `json:"vehicle_id"`
	Type      domain.ActionType `json:"action_type"`
	Note      *string           `json:"note"`
	CreatedBy string            `json:"created_by"`
	CreatedAt time.Time         `json:"created_at"`
}

// Log implements domain.ActionRepo in a single transaction.
func (r *ActionRepo) Log(ctx context.Context, vehicleID uuid.UUID, a domain.NewAction, idemKey, reqHash string) (domain.Action, bool, error) {
	ctx, cancel := r.ctx(ctx)
	defer cancel()
	var (
		out    domain.Action
		replay bool
	)
	err := withTx(ctx, r.pool, pgx.TxOptions{}, func(q *gen.Queries) error {
		// Serialize requests sharing a key so only one of them can win.
		if err := q.LockIdempotencyKey(ctx, idemKey); err != nil {
			return err
		}
		rec, err := q.GetIdempotencyKey(ctx, idemKey)
		switch {
		case err == nil:
			if rec.RequestHash != reqHash {
				return domain.Conflictf("Idempotency-Key was already used with a different request")
			}
			var sa storedAction
			if err := json.Unmarshal(rec.ResponseBody, &sa); err != nil {
				return fmt.Errorf("decode idempotency record: %w", err)
			}
			out, replay = domain.Action(sa), true
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}

		status, err := q.LockVehicle(ctx, vehicleID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NotFoundf("vehicle %s not found", vehicleID)
		}
		if err != nil {
			return err
		}
		if domain.VehicleStatus(status) != domain.StatusInStock {
			return domain.Conflictf("vehicle %s is not in stock", vehicleID)
		}
		row, err := q.InsertAction(ctx, gen.InsertActionParams{
			ID:         a.ID,
			VehicleID:  vehicleID,
			ActionType: gen.ActionType(a.Type),
			Note:       optText(a.Note),
			CreatedBy:  a.CreatedBy,
		})
		if err != nil {
			return err
		}
		if err := q.SetCurrentAction(ctx, gen.SetCurrentActionParams{
			ID:         vehicleID,
			ActionType: gen.NullActionType{ActionType: row.ActionType, Valid: true},
			ActionAt:   row.CreatedAt,
		}); err != nil {
			return err
		}
		out = toAction(row)
		body, err := json.Marshal(storedAction(out))
		if err != nil {
			return err
		}
		return q.InsertIdempotencyKey(ctx, gen.InsertIdempotencyKeyParams{
			Key:            idemKey,
			RequestHash:    reqHash,
			ResponseStatus: http.StatusCreated,
			ResponseBody:   body,
		})
	})
	if err != nil {
		return domain.Action{}, false, fmt.Errorf("log action: %w", err)
	}
	return out, replay, nil
}

// History implements domain.ActionRepo.
func (r *ActionRepo) History(ctx context.Context, vehicleID uuid.UUID, limit int) ([]domain.Action, error) {
	ctx, cancel := r.ctx(ctx)
	defer cancel()
	rows, err := r.q.ListActions(ctx, gen.ListActionsParams{VehicleID: vehicleID, Lim: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("list actions: %w", err)
	}
	if len(rows) == 0 {
		exists, err := r.q.VehicleExists(ctx, vehicleID)
		if err != nil {
			return nil, fmt.Errorf("vehicle exists: %w", err)
		}
		if !exists {
			return nil, domain.NotFoundf("vehicle %s not found", vehicleID)
		}
	}
	out := make([]domain.Action, len(rows))
	for i, row := range rows {
		out[i] = toAction(row)
	}
	return out, nil
}

// SettingsRepo implements domain.SettingsRepo.
type SettingsRepo struct{ base }

var _ domain.SettingsRepo = (*SettingsRepo)(nil)

// Threshold implements domain.SettingsRepo.
func (r *SettingsRepo) Threshold(ctx context.Context) (int, error) {
	ctx, cancel := r.ctx(ctx)
	defer cancel()
	days, err := r.q.GetAgingThreshold(ctx)
	if err != nil {
		return 0, fmt.Errorf("get threshold: %w", err)
	}
	return int(days), nil
}

// SetThreshold implements domain.SettingsRepo.
func (r *SettingsRepo) SetThreshold(ctx context.Context, days int) error {
	ctx, cancel := r.ctx(ctx)
	defer cancel()
	if err := r.q.SetAgingThreshold(ctx, int32(days)); err != nil {
		return fmt.Errorf("set threshold: %w", err)
	}
	return nil
}

func toVehicleRow(v gen.Vehicle) (domain.VehicleRow, error) {
	price, err := numericToCents(v.Price)
	if err != nil {
		return domain.VehicleRow{}, fmt.Errorf("vehicle %s price: %w", v.ID, err)
	}
	row := domain.VehicleRow{
		Vehicle: domain.Vehicle{
			ID:         v.ID,
			VIN:        v.Vin,
			Make:       v.Make,
			Model:      v.Model,
			Year:       int(v.Year),
			PriceCents: price,
			ReceivedAt: v.ReceivedAt.Time,
			Status:     domain.VehicleStatus(v.Status),
		},
		CurrentAction: fromNullAction(v.CurrentActionType),
	}
	if v.CurrentActionAt.Valid {
		t := v.CurrentActionAt.Time
		row.CurrentActionAt = &t
	}
	return row, nil
}

func toAction(a gen.VehicleAction) domain.Action {
	out := domain.Action{
		ID:        a.ID,
		VehicleID: a.VehicleID,
		Type:      domain.ActionType(a.ActionType),
		CreatedBy: a.CreatedBy,
		CreatedAt: a.CreatedAt.Time,
	}
	if a.Note.Valid {
		n := a.Note.String
		out.Note = &n
	}
	return out
}

func fromNullAction(n gen.NullActionType) *domain.ActionType {
	if !n.Valid {
		return nil
	}
	t := domain.ActionType(n.ActionType)
	return &t
}

func optText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func optDate(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: domain.DateOf(*t), Valid: true}
}

func centsToNumeric(c *int64) pgtype.Numeric {
	if c == nil {
		return pgtype.Numeric{}
	}
	return pgtype.Numeric{Int: big.NewInt(*c), Exp: -2, Valid: true}
}

func numericToCents(n pgtype.Numeric) (*int64, error) {
	if !n.Valid {
		return nil, nil
	}
	if n.NaN || n.InfinityModifier != pgtype.Finite {
		return nil, errors.New("non-finite numeric")
	}
	// value = Int * 10^Exp, so cents = Int * 10^(Exp+2).
	i := new(big.Int).Set(n.Int)
	if e := int64(n.Exp) + 2; e >= 0 {
		i.Mul(i, new(big.Int).Exp(big.NewInt(10), big.NewInt(e), nil))
	} else {
		i.Quo(i, new(big.Int).Exp(big.NewInt(10), big.NewInt(-e), nil))
	}
	if !i.IsInt64() {
		return nil, errors.New("numeric out of range")
	}
	v := i.Int64()
	return &v, nil
}
