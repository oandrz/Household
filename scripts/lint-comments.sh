#!/usr/bin/env bash
# Joins each run of consecutive whole-line comments into one block before
# matching, so a reference split across lines ("decision" / "// 7") cannot
# escape a plain per-line grep -- that has already happened once in this
# sweep. A comment trailing code on the same line (a struct field like
# `Kind string `json:"kind"` // ... (decision 21)`, the exact shape this
# codebase used before the sweep) can never itself span two lines, so it is
# matched against its own text directly instead of feeding the block
# machine. The join needs a per-line state machine (function calls, a
# file-boundary reset, per-line offsets so a split reference is blamed on
# the line the match starts on) that would be unreadable once every $ in it
# had to be doubled for Make's own variable syntax, so it lives here instead
# of inline in the Makefile recipe.
set -euo pipefail

cd "$(dirname "$0")/.."

go_files=$(find api -name '*.go')
sql_files=$(find api/internal/adapter/postgres/queries -name '*.sql')

# The two alternatives of HISTORY_REF, kept apart so the whole-line block
# match and the single-line trailing-comment match share one definition
# instead of two copies drifting apart.
PAT1='([Dd]ecision|[Tt]ask|[Mm]ilestone)[[:space:]]+[0-9]'
PAT2='\.(go|sql|ts|tsx):[0-9]'

out=$(awk -v pat1="$PAT1" -v pat2="$PAT2" '
  function trim(s) {
    sub(/^[ \t]+/, "", s)
    return s
  }
  function matches(s) {
    return (s ~ pat1) || (s ~ pat2)
  }
  # The leftmost position where either alternative starts to match, or 0 if
  # neither does -- this is what lets a split reference be blamed on the
  # line where the match actually begins, not just the block first line.
  function matchstart(s,    best) {
    best = 0
    if (match(s, pat1)) best = RSTART
    if (match(s, pat2)) {
      if (best == 0 || RSTART < best) best = RSTART
    }
    return best
  }
  # A block ends when the run of whole-line comments does (or the file
  # does); only then is the joined text checked, so a reference split across
  # lines is still caught. The marker itself is dropped from each line
  # before joining -- otherwise a split like "// see decision" / "// 4 for
  # why" joins into "see decision // 4 for why", and the // sits between
  # "decision" and "4" where the pattern expects only whitespace.
  function flush(fname) {
    if (marker != "" && matches(block)) {
      pos = matchstart(block)
      # lineoff[i] is where line i contribution starts inside block; the
      # last one at or before pos is the line the match itself starts on,
      # which can be later than the block own first line.
      matchline = lineno[1]
      for (i = 1; i <= nlines; i++) {
        if (lineoff[i] <= pos) matchline = lineno[i]
      }
      print fname ":" matchline ": " block
    }
    marker = ""
    block = ""
    nlines = 0
  }
  FNR == 1 {
    # FILENAME already names the file about to start, so the block still
    # open from the end of the PREVIOUS file must be flushed under the name
    # saved in curfile, not FILENAME -- otherwise a trailing unclosed block
    # is blamed on whichever file happens to come next.
    flush(curfile)
    curfile = FILENAME
    allow_slash = (FILENAME ~ /\.go$/)
    # sqlcgen copies the queries verbatim into its Go string literals, one
    # source line per generated line, so the SQL "--" comments inside those
    # literals need the same scan as the .sql files they came from.
    allow_dash = (FILENAME ~ /\.sql$/) || (FILENAME ~ /\/sqlcgen\//)
  }
  {
    t = trim($0)
    m = ""
    if (allow_slash && substr(t, 1, 2) == "//") m = "//"
    else if (allow_dash && substr(t, 1, 2) == "--") m = "--"

    if (m != "") {
      body = trim(substr(t, 3))
      if (m == marker) {
        block = block " " body
        nlines++
        lineno[nlines] = FNR
        lineoff[nlines] = length(block) - length(body) + 1
      } else {
        flush(curfile)
        marker = m
        block = body
        nlines = 1
        lineno[1] = FNR
        lineoff[1] = 1
      }
    } else {
      flush(curfile)
      # A trailing comment (code, then // or -- further along the same
      # line) can never span two lines, so it is checked on its own rather
      # than joined. Requiring whitespace right before the marker tells a
      # real comment apart from one that is itself inside a string literal
      # such as "https://...", where the slashes are preceded by a colon,
      # not whitespace.
      tpos = 0
      if (allow_slash) tpos = match(t, /[ \t]\/\//)
      if (tpos == 0 && allow_dash) tpos = match(t, /[ \t]--/)
      if (tpos > 0) {
        trailing = trim(substr(t, tpos + RLENGTH))
        if (matches(trailing)) print curfile ":" FNR ": " trailing
      }
    }
  }
  END { flush(curfile) }
' $go_files $sql_files)

if [ -n "$out" ]; then
  echo "$out"
  echo "lint-comments: state the rule, not a plan number or a line number (CLAUDE.md, Comments)"
  exit 1
fi
echo "lint-comments passed"
