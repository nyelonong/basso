;; indo-bounce.fnl — Indonesian-bounce/funkot-style groove: fast tempo,
;; syncopated "jedag-jedug" double-kick bounce (a tresillo-ish 3+3+2 feel),
;; driving 16th-note hihat with an open-hat lift into the next bar, a big
;; clap+snare backbeat, and a sub-bass that rides the kick rhythm.
;; Try it: `basso play patterns/indo-bounce.fnl`.

(bpm 150)
(steps 16)

(fn pattern [bar]
  [;; kick: 3+3+2, 3+3+2 — the bounce
   {:step 0 :sample "kick2.wav"}
   {:step 3 :sample "kick2.wav"}
   {:step 6 :sample "kick2.wav"}
   {:step 8 :sample "kick2.wav"}
   {:step 11 :sample "kick2.wav"}
   {:step 14 :sample "kick2.wav"}

   ;; backbeat: snare + clap layered for a bigger hit
   {:step 4 :sample "snare.wav"}
   {:step 4 :sample "handclap.wav"}
   {:step 12 :sample "snare.wav"}
   {:step 12 :sample "handclap.wav"}

   ;; hihat: driving 16ths, accented where the kick lands, open hat lift
   ;; into the next bar
   {:step 0 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 1 :sample "cl_hihat.wav" :velocity 0.6}
   {:step 2 :sample "cl_hihat.wav" :velocity 0.6}
   {:step 3 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 4 :sample "cl_hihat.wav" :velocity 0.6}
   {:step 5 :sample "cl_hihat.wav" :velocity 0.6}
   {:step 6 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 7 :sample "cl_hihat.wav" :velocity 0.6}
   {:step 8 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 9 :sample "cl_hihat.wav" :velocity 0.6}
   {:step 10 :sample "cl_hihat.wav" :velocity 0.6}
   {:step 11 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 12 :sample "cl_hihat.wav" :velocity 0.6}
   {:step 13 :sample "cl_hihat.wav" :velocity 0.6}
   {:step 14 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 15 :sample "open_hh.wav" :velocity 0.8}

   ;; sub-bass: rides the kick's 3+3+2 rhythm
   {:step 0 :note "A1" :length 3}
   {:step 3 :note "A1" :length 3}
   {:step 6 :note "A1" :length 2}
   {:step 8 :note "A1" :length 3}
   {:step 11 :note "A1" :length 3}
   {:step 14 :note "A1" :length 2}])
