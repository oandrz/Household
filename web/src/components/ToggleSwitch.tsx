// The on/off pill from the design (design/Household Dashboard.dc.html's
// Settings screen and every modal's boolean fields: "Show IDR equivalents",
// the four notification rows, the Invite-member modal's four capability
// rows). It appears at more than a dozen call sites across the Settings
// screen alone, so it lives here rather than being copied into each panel --
// the same reasoning components/Modal.tsx's header comment gives for why it
// lives outside a feature folder.
//
// Built as a real <button role="switch"> (not the design's plain styled
// <div>) so it is keyboard-operable and has a queryable checked state for
// tests -- `getByRole("switch", { name, checked })`.
export function ToggleSwitch({
  checked,
  onChange,
  disabled = false,
  label,
}: {
  checked: boolean;
  onChange: () => void;
  disabled?: boolean;
  label: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={onChange}
      className={`flex h-[23px] w-10 flex-none items-center rounded-full p-0.5 transition-colors disabled:cursor-not-allowed disabled:opacity-60 ${
        checked ? "justify-end bg-accent" : "justify-start bg-toggle-off"
      }`}
    >
      <span className="h-[19px] w-[19px] rounded-full bg-white" />
    </button>
  );
}
