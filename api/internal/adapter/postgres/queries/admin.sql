-- name: GetPlatformAdmin :one
SELECT user_id, note, created_at FROM platform_admins WHERE user_id = $1;

-- name: GrantPlatformAdmin :exec
INSERT INTO platform_admins (user_id, note) VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET note = EXCLUDED.note;

-- name: RevokePlatformAdmin :exec
DELETE FROM platform_admins WHERE user_id = $1;

-- name: ListPlatformAdmins :many
SELECT pa.user_id, u.email, u.display_name, pa.note, pa.created_at
FROM platform_admins pa
JOIN users u ON u.id = pa.user_id
ORDER BY pa.created_at;

-- name: GetGlobalFlagOverrides :many
SELECT key, enabled FROM feature_flags;

-- name: GetHouseholdFlagOverrides :many
SELECT key, enabled FROM household_feature_flags WHERE household_id = $1;

-- name: ListHouseholdFlagOverrides :many
SELECT hff.household_id, h.name AS household_name, hff.key, hff.enabled
FROM household_feature_flags hff
JOIN households h ON h.id = hff.household_id
ORDER BY h.name, hff.key;

-- name: SetGlobalFlag :exec
INSERT INTO feature_flags (key, enabled, updated_by) VALUES ($1, $2, $3)
ON CONFLICT (key) DO UPDATE
  SET enabled = EXCLUDED.enabled, updated_at = now(), updated_by = EXCLUDED.updated_by;

-- name: SetHouseholdFlag :exec
INSERT INTO household_feature_flags (household_id, key, enabled, updated_by) VALUES ($1, $2, $3, $4)
ON CONFLICT (household_id, key) DO UPDATE
  SET enabled = EXCLUDED.enabled, updated_at = now(), updated_by = EXCLUDED.updated_by;

-- name: ClearHouseholdFlag :exec
DELETE FROM household_feature_flags WHERE household_id = $1 AND key = $2;

-- name: RecordAdminAudit :exec
INSERT INTO admin_audit_log (actor_user_id, action, target, detail, ip, created_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: RecentAdminAudit :many
SELECT actor_user_id, action, target, detail, ip, created_at
FROM admin_audit_log ORDER BY created_at DESC LIMIT $1;

-- name: RecordAdminReauthAttempt :exec
INSERT INTO admin_reauth_attempts (user_id, succeeded, at) VALUES ($1, $2, $3);

-- name: AdminReauthFailuresSince :many
SELECT at FROM admin_reauth_attempts
WHERE user_id = $1 AND succeeded = false AND at >= $2
ORDER BY at;

-- name: ClearAdminReauthFailures :exec
DELETE FROM admin_reauth_attempts WHERE user_id = $1;
