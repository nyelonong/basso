;; funk-bass-groove.fnl — syncopated 16th-note funk: tight closed hihat,
;; backbeat snare with a ghost note, and a punchy bass line built from short
;; stabs, a passing tone, and an octave pop.
;; Try it: `basso play patterns/funk-bass-groove.fnl`.

(bpm 100)
(steps 16)

(fn pattern [bar]
  [;; hihat: 16th notes throughout, accented on the 8th-note pulse
   {:step 0 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 1 :sample "cl_hihat.wav" :velocity 0.5}
   {:step 2 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 3 :sample "cl_hihat.wav" :velocity 0.5}
   {:step 4 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 5 :sample "cl_hihat.wav" :velocity 0.5}
   {:step 6 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 7 :sample "cl_hihat.wav" :velocity 0.5}
   {:step 8 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 9 :sample "cl_hihat.wav" :velocity 0.5}
   {:step 10 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 11 :sample "cl_hihat.wav" :velocity 0.5}
   {:step 12 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 13 :sample "cl_hihat.wav" :velocity 0.5}
   {:step 14 :sample "cl_hihat.wav" :velocity 1.0}
   {:step 15 :sample "cl_hihat.wav" :velocity 0.5}

   ;; kick and backbeat snare, syncopated
   {:step 0 :sample "kick2.wav"}
   {:step 6 :sample "kick2.wav"}
   {:step 10 :sample "kick2.wav"}
   {:step 4 :sample "snare.wav"}
   {:step 12 :sample "snare.wav"}
   {:step 14 :sample "snare.wav" :velocity 0.3} ;; ghost note

   ;; bass: root E1, short punchy stabs, a passing tone, and an octave pop
   {:step 0 :note "E1" :length 1}
   {:step 3 :note "E1" :length 1 :velocity 0.5} ;; ghost
   {:step 6 :note "E1" :length 1}
   {:step 7 :note "G1" :length 1 :velocity 0.5} ;; passing tone
   {:step 8 :note "E2" :length 1} ;; octave pop
   {:step 10 :note "E1" :length 2}
   {:step 14 :note "B1" :length 1}])
