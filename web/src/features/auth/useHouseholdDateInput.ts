// The state behind a date field that should start on the household's today:
// a purchase date, a payment date, a transaction date.
//
// Use it wherever a form would write `useState(today())`. It returns the same
// pair useState does, with one difference: pass null to the setter to put the
// field back on today (after a successful save, say).
import { useState } from "react";
import { todayIn } from "../../lib/householdDate";
import { useHouseholdZone } from "./useHouseholdZone";

export function useHouseholdDateInput(
  initial: string | null = null,
): [string, (value: string | null) => void] {
  const today = todayIn(useHouseholdZone());
  // null means "the person has not chosen a date". Today is then worked out
  // on every render rather than stored once at mount, so the field is on the
  // household's day even if the household's zone arrives after the form has
  // mounted, and it moves on at midnight in a form left open.
  //
  // "" is different: that is a field the person emptied, and it stays empty.
  const [chosen, setChosen] = useState<string | null>(initial);
  return [chosen ?? today, setChosen];
}
