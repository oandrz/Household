// Mirrors api/internal/domain/text_limits.go. The server is the authority;
// these only stop a person typing past a limit the server would refuse.
// The browser's maxLength counts UTF-16 code units, so an emoji uses two:
// a field full of emoji stops a little early here, never late.
export const MAX_NAME_LENGTH = 80;
export const MAX_TRANSACTION_DESCRIPTION_LENGTH = 200;
export const MAX_NOTE_LENGTH = 500;
export const MAX_HOLDING_UNIT_LENGTH = 20;
