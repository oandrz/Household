package postgres

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// text converts a domain string that is always present into the pointer
// sqlc expects for a nullable column. It is for lookups (ByEmail,
// CountSince) where the caller is always searching for a real value, never
// for NULL — unlike nullableText below, it never returns nil.
func text(s string) *string { return &s }

// nullableText implements the "" <-> SQL NULL convention documented on
// usecase.StoredUser.PasswordHash and UserRepository.Create: an empty domain
// string is stored as NULL, never as an empty-string column value.
// users.email is citext UNIQUE and nullable for the same reason as
// password_hash -- a household's children have no email of their own, and
// storing "" for each would collide on the unique index where NULL does
// not.
func nullableText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// stringOrEmpty is nullableText's inverse: SQL NULL comes back as "".
func stringOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// nullableInt8 is nullableText's counterpart for invites.knock_chat_id. A
// chat id is never 0 -- migration 00021's invites_knock_chat_is_a_person
// requires it positive -- so 0 stands for "no chat" the same way ""
// stands for "no string" above.
func nullableInt8(n int64) *int64 {
	if n == 0 {
		return nil
	}
	return &n
}

// uuid parses a domain id into the wire type. Domain ids only ever
// originate from a row this package itself produced, so a parse failure
// here can only mean a caller passed a malformed id — the resulting query
// simply matches no row, and translate turns that into domain.ErrNotFound
// like any other miss, rather than panicking or silently corrupting data.
func uuid(id string) pgtype.UUID {
	var u pgtype.UUID
	_ = u.Scan(id)
	return u
}

// nullableUUID is uuid's counterpart for the few columns that are genuinely
// optional at the schema level (login_attempts.household_id and .user_id,
// and accounts.owner_membership_id), where the port passes *string and a nil
// pointer must reach Postgres as NULL, not as the zero UUID.
func nullableUUID(id *string) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return uuid(*id)
}

// uuidLooksValid reports whether id parses as a UUID, without the silent
// fall-through to the zero value uuid() and nullableUUID() use. Their "a
// malformed id just matches no row" reasoning holds for an ordinary equality
// comparison, but breaks for a column compared with "$n IS NULL OR ...":
// there, a parse failure and a genuinely absent value both produce the same
// zero pgtype.UUID{}, so only a caller that checks before the id reaches SQL
// can tell them apart. That case is retro_actions.carried_from (see
// AddRetroAction's own comment), where the id can arrive from a request body
// this package did not construct -- unlike every other id here, which
// originates from a row this package already produced.
func uuidLooksValid(id string) bool {
	var u pgtype.UUID
	return u.Scan(id) == nil
}

func uuidToString(u pgtype.UUID) string { return u.String() }

// uuidOrEmpty renders a nullable uuid column as "" rather than the zero
// UUID's "00000000-...", so a caller can test it with a plain == "".
func uuidOrEmpty(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return u.String()
}

// int64Or renders a nullable bigint column (emit_pointers_for_null_types
// gives it *int64) as 0 for NULL, the same "absent means zero value"
// convention timeOf and stringOrEmpty give their own nullable columns.
func int64Or(n *int64) int64 {
	if n == nil {
		return 0
	}
	return *n
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func timeOf(t pgtype.Timestamptz) time.Time { return t.Time }

// timePtrOf converts a nullable timestamptz into the *time.Time the ports
// use for optional times (InviteDetails.AcceptedAt).
func timePtrOf(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	tm := t.Time
	return &tm
}

func toTimes(rows []pgtype.Timestamptz) []time.Time {
	out := make([]time.Time, len(rows))
	for i, r := range rows {
		out[i] = timeOf(r)
	}
	return out
}

// toStoredUser assembles the one user shape every repository method that
// touches the users table returns, converting sqlc's wire types
// (pgtype.UUID, nullable *string columns) into the domain/usecase boundary
// shapes in one place.
func toStoredUser(id pgtype.UUID, email, passwordHash *string, displayName, avatarInitial string) usecase.StoredUser {
	return usecase.StoredUser{
		User: domain.User{
			ID:            uuidToString(id),
			Email:         stringOrEmpty(email),
			DisplayName:   displayName,
			AvatarInitial: avatarInitial,
		},
		PasswordHash: stringOrEmpty(passwordHash),
	}
}

func toDomainHousehold(id pgtype.UUID, name, familyName, primaryCurrency string,
	showSecondaryCurrency bool, secondaryCurrency, fxRateMode string) domain.Household {
	return domain.Household{
		ID:                    uuidToString(id),
		Name:                  name,
		FamilyName:            familyName,
		PrimaryCurrency:       primaryCurrency,
		ShowSecondaryCurrency: showSecondaryCurrency,
		SecondaryCurrency:     secondaryCurrency,
		FXRateMode:            fxRateMode,
	}
}

func toDomainSpace(row sqlcgen.Space) domain.Space {
	return domain.Space{
		ID:                 uuidToString(row.ID),
		HouseholdID:        uuidToString(row.HouseholdID),
		Key:                row.Key,
		Name:               row.Name,
		Visibility:         domain.Visibility(row.Visibility),
		Position:           int(row.Position),
		IsBuiltin:          row.IsBuiltin,
		RequiredCapability: domain.Capability(row.RequiredCapability),
	}
}

func toNotificationPreferences(row sqlcgen.NotificationPreference) usecase.NotificationPreferences {
	return usecase.NotificationPreferences{
		BillReminders:   row.BillReminders,
		OverspendAlerts: row.OverspendAlerts,
		RetroReminder:   row.RetroReminder,
		WeeklyDigest:    row.WeeklyDigest,
	}
}

// toRole and toCapabilities refuse values this code did not write -- the
// fail-closed rule every other enum column read in this package follows
// (toBill's cadence, toCategory's kind, admin_directory_repo's member roles).
// migrations/00002_identity.sql's CHECK constraints are the first gate;
// parsing on read is the second, and costs only a string compare.
//
// Don't trust the CHECK alone: a role is the input to authorisation at
// every inbound edge, so an impossible one must fail the request, not reach
// a guard that compares it with "owner" and quietly treats it as something
// else.
//
// domain.Space.RequiredCapability's "" (no capability required) is a
// legitimate stored value that ParseCapabilities would refuse, which is why
// toDomainSpace does not go through toCapabilities.
func toRole(s string) (domain.Role, error) {
	role, err := domain.ParseRole(s)
	if err != nil {
		return "", fmt.Errorf("postgres: membership role: %w", err)
	}
	return role, nil
}

func toCapabilities(ss []string) (domain.Capabilities, error) {
	caps, err := domain.ParseCapabilities(ss)
	if err != nil {
		return nil, fmt.Errorf("postgres: membership capabilities: %w", err)
	}
	return caps, nil
}

// toMembership builds a domain.Membership from the columns every membership
// query returns. It takes plain fields rather than a generated row struct for
// the reason toGoal gives: each query has its own sqlc row type over the same
// columns.
func toMembership(id, householdID, userID pgtype.UUID, role string, capabilities []string) (domain.Membership, error) {
	r, err := toRole(role)
	if err != nil {
		return domain.Membership{}, err
	}
	caps, err := toCapabilities(capabilities)
	if err != nil {
		return domain.Membership{}, err
	}
	return domain.Membership{
		ID:           uuidToString(id),
		HouseholdID:  uuidToString(householdID),
		UserID:       uuidToString(userID),
		Role:         r,
		Capabilities: caps,
	}, nil
}

// dateOnly converts a domain time into the pgtype.Date that
// opening_balance_as_of is stored as. The column is a date, not a
// timestamptz, on purpose: "the balance was true on the 26th" is a calendar
// fact independent of the zone the request arrived from.
//
// Don't replace t.Date() with t.UTC().Truncate(24*time.Hour): it converts to
// UTC before truncating, so it silently changes the calendar day for
// anything not already UTC midnight (07:00 SGT on the 26th truncates to the
// 25th). Every caller happens to pass UTC midnight today, which is exactly
// why that bug shipped once with all tests green; see
// TestOpeningBalanceAsOfKeepsItsCalendarDayRegardlessOfZone.
func dateOnly(t time.Time) pgtype.Date {
	y, m, d := t.Date()
	return pgtype.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), Valid: true}
}

func dateToTime(d pgtype.Date) time.Time { return d.Time }

// nullableDate is dateOnly's counterpart for the columns that are genuinely
// optional -- goals.target_month, goal_contributions.source_budget_month and
// bills.next_due (NULL there means a settled one-off, 00008_bills.sql's own
// comment) -- where the port passes *time.Time and a nil pointer must reach
// Postgres as NULL, the same "" <-> SQL NULL shape nullableUUID gives ids.
func nullableDate(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	return dateOnly(*t)
}

// dateToTimePtr is nullableDate's read-side counterpart: a NULL date column
// comes back as nil, never the zero time, so a caller (GoalRepository.Get's
// own doc comment: "not the zero time") cannot mistake "never set" for
// "set to January 1, year 1."
func dateToTimePtr(d pgtype.Date) *time.Time {
	if !d.Valid {
		return nil
	}
	t := dateToTime(d)
	return &t
}

// Compile-time confirmation that every repository satisfies its port: a
// signature drift from a usecase port fails the build here, in the adapter
// that drifted, rather than at whichever caller first wires it.
var (
	_ usecase.UserRepository         = (*UserRepo)(nil)
	_ usecase.HouseholdRepository    = (*HouseholdRepo)(nil)
	_ usecase.MembershipRepository   = (*MembershipRepo)(nil)
	_ usecase.SessionRepository      = (*SessionRepo)(nil)
	_ usecase.MagicLinkRepository    = (*MagicLinkRepo)(nil)
	_ usecase.LoginAttemptRepository = (*LoginAttemptRepo)(nil)
	_ usecase.InviteRepository       = (*InviteRepo)(nil)
	_ usecase.SpaceRepository        = (*SpaceRepo)(nil)
	_ usecase.NotificationRepository = (*NotificationRepo)(nil)
	_ usecase.SignupRepository       = (*SignupRepo)(nil)
	_ usecase.AccountRepository      = (*AccountRepo)(nil)
	_ usecase.CategoryRepository     = (*CategoryRepo)(nil)
	_ usecase.TransactionRepository  = (*TransactionRepo)(nil)
	_ usecase.BudgetRepository       = (*BudgetRepo)(nil)
	_ usecase.GoalRepository         = (*GoalRepo)(nil)
	_ usecase.BillRepository         = (*BillRepo)(nil)
	_ usecase.RetroRepository        = (*RetroRepo)(nil)
	_ usecase.RetroActionRepository  = (*RetroActionRepo)(nil)
	_ usecase.AgreementRepository    = (*AgreementRepo)(nil)

	_ usecase.PlatformAdminRepository      = (*PlatformAdminRepo)(nil)
	_ usecase.FeatureFlagRepository        = (*FeatureFlagRepo)(nil)
	_ usecase.AdminAuditRepository         = (*AdminAuditRepo)(nil)
	_ usecase.AdminReauthAttemptRepository = (*AdminReauthAttemptRepo)(nil)
)
