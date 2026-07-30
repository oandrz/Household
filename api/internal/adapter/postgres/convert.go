package postgres

import (
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
// usecase.StoredUser.PasswordHash and UserRepository.Create: an empty
// domain string is stored as NULL, never as an empty-string column value.
// users.email is citext UNIQUE and nullable for the same reason
// password_hash is nullable — a household's children are created with no
// email of their own, and storing ” for each of them would collide on the
// unique index where storing NULL does not. The brief's own test
// (`users.Create(ctx, "", "", "Ethan")`) exercises exactly this path, so the
// convention is applied to both columns even though ports.go's doc comment
// only spells it out for PasswordHash.
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

func uuidToString(u pgtype.UUID) string { return u.String() }

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

// toRole and toCapabilities convert already-validated database columns
// directly: migrations/00002_identity.sql's CHECK constraints (role,
// capabilities_are_known, limited_members_have_no_marriage,
// owners_hold_all_capabilities) are the enforcement point for rows already
// in the table, so re-running domain.ParseRole / domain.ParseCapabilities on
// every read would just repeat a check the schema already guarantees — and
// ParseCapabilities would reject an empty slice's caller-facing sibling,
// domain.Space.RequiredCapability's "" for no-capability-required, which is
// a legitimate stored value (see toDomainSpace) but not a legitimate member
// of Capabilities.
func toRole(s string) domain.Role { return domain.Role(s) }

func toCapabilities(ss []string) domain.Capabilities {
	caps := make(domain.Capabilities, len(ss))
	for i, s := range ss {
		caps[i] = domain.Capability(s)
	}
	return caps
}

// dateOnly converts a domain time into the pgtype.Date that
// opening_balance_as_of is stored as. The column is a date, not a
// timestamptz, deliberately: "the balance was true on the 26th" is a calendar
// fact, and storing an instant would make it depend on the zone the request
// arrived from.
//
// t.Date() is why this holds: it reads the calendar day out of t in t's own
// location, before any conversion happens. t.UTC().Truncate(24*time.Hour)
// looks equivalent but is not -- it converts to UTC first and truncates
// second, so it silently changes the calendar day for anything not already
// UTC midnight (07:00 SGT on the 26th is 23:00 UTC on the 25th, and
// truncating that lands on the 25th). Every existing caller happens to pass
// UTC midnight today, which is exactly why that bug shipped once already
// with all tests green; see
// TestOpeningBalanceAsOfKeepsItsCalendarDayRegardlessOfZone.
func dateOnly(t time.Time) pgtype.Date {
	y, m, d := t.Date()
	return pgtype.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), Valid: true}
}

func dateToTime(d pgtype.Date) time.Time { return d.Time }

// Compile-time confirmation that every repository satisfies its port.
// Nothing in internal/usecase constructs these yet -- that is Task 12's job
// -- so without this, a signature drift from ports.go would not surface
// until then.
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
)
