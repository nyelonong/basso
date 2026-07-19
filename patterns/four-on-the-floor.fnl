;; four-on-the-floor.fnl — an uptempo house pattern: kick on every quarter
;; note, open hihat on the off-beats, clap layered on 2 and 4, explicit pan
;; and velocity to show those fields aren't just left to the random default.

(bpm 128)
(steps 16)

(fn pattern [bar]
  [{:step 0 :sample "kick2.wav" :velocity 1.0}
   {:step 4 :sample "kick2.wav" :velocity 1.0}
   {:step 8 :sample "kick2.wav" :velocity 1.0}
   {:step 12 :sample "kick2.wav" :velocity 1.0}

   {:step 4 :sample "handclap.wav" :pan 0.0 :velocity 0.9}
   {:step 12 :sample "handclap.wav" :pan 0.0 :velocity 0.9}

   {:step 2 :sample "open_hh.wav" :pan -0.3 :velocity 0.7}
   {:step 6 :sample "open_hh.wav" :pan 0.3 :velocity 0.7}
   {:step 10 :sample "open_hh.wav" :pan -0.3 :velocity 0.7}
   {:step 14 :sample "open_hh.wav" :pan 0.3 :velocity 0.7}])
