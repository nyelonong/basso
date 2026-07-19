;; generative.fnl — shows why patterns are code, not just data: a fixed
;; kick/snare skeleton plus a hi-hat line built by a loop, with an accented
;; (louder) velocity every 4th step instead of hand-writing each hit.

(bpm 100)
(steps 16)

(fn pattern [bar]
  (let [hits [{:step 0 :sample "kick2.wav"}
              {:step 8 :sample "kick2.wav"}
              {:step 4 :sample "snare.wav"}
              {:step 12 :sample "snare.wav"}]]
    (for [i 0 15 2]
      (table.insert hits {:step i
                           :sample "cl_hihat.wav"
                           :velocity (if (= (% i 4) 0) 1.0 0.55)}))
    hits))
