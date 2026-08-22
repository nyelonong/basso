# Backlog

- Promote `basso studio` to the default face of playback after it bakes in (spec decision: new subcommand first).
- Step-grid sequencer visualization in studio (spec non-goal for v1; revisit if the read-only hit view proves wanted).
- Extend the suggestion repair loop to cover unparseable model output (e.g. `source` not a string): stealth/free endpoints emit malformed JSON intermittently; today the user must manually resubmit.
- Handle SIGHUP / parent-terminal loss in play and studio so an orphaned process cannot keep holding the audio device after its controlling pty dies.
