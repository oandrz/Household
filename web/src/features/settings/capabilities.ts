// The capability set every owner must hold. Mirrors
// api/internal/domain/identity.go's AllCapabilities() -- the server is
// authoritative and enforces this independently
// (domain.ErrOwnerMustHoldAllCapabilities on any owner write that omits
// one). Defined once here, rather than as a separate literal in
// InviteMemberModal and MembersPanel, so a fifth capability being added
// later means updating one value instead of two silently drifting apart.
export const ALL_CAPABILITIES: readonly string[] = ["calendar", "chores", "money", "marriage"];
