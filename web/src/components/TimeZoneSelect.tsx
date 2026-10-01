// A <select> of IANA time zones, used where a household's zone is chosen:
// Settings, and the sign-up form when the browser's own zone cannot be used.
import { useMemo } from "react";
import { timeZoneOptions } from "../lib/timeZoneOptions";

export function TimeZoneSelect({
  id,
  value,
  onChange,
  disabled = false,
  className,
}: {
  id: string;
  value: string;
  onChange: (zone: string) => void;
  disabled?: boolean;
  className?: string;
}) {
  // The current value is always among the options, even when the browser
  // does not list it -- see timeZoneOptions. Recomputed only when the value
  // changes: there are over four hundred zones and each label formats a date.
  const options = useMemo(() => timeZoneOptions([value]), [value]);

  return (
    <select
      id={id}
      value={value}
      disabled={disabled}
      onChange={(event) => onChange(event.target.value)}
      className={className}
    >
      {/* Only when nothing is chosen yet. A real zone is never replaced by
          this: it is in the list above. */}
      {value === "" && <option value="">Choose a time zone</option>}
      {options.map((option) => (
        <option key={option.zone} value={option.zone}>
          {option.label}
        </option>
      ))}
    </select>
  );
}
