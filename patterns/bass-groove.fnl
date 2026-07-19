;; bass-groove.fnl — drums plus a synthesized bass line.
;; :note/:length trigger the synthesized bass instrument (sawtooth +
;; short attack/release envelope) instead of a WAV sample; :length is in
;; steps. Try it: `basso play patterns/bass-groove.fnl`.

(bpm 100)
(steps 16)

(fn pattern [bar]
  [{:step 0 :sample "kick2.wav"}
   {:step 4 :sample "snare.wav"}
   {:step 6 :sample "cl_hihat.wav"}
   {:step 8 :sample "kick2.wav"}
   {:step 12 :sample "snare.wav"}
   {:step 14 :sample "cl_hihat.wav"}
   {:step 0 :note "C2" :length 4}
   {:step 4 :note "C2" :length 2}
   {:step 8 :note "Eb2" :length 4}
   {:step 12 :note "G2" :length 4}])
