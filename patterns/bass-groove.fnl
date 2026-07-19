;; bass-groove.fnl — drums plus a synthesized bass line, a brass swell, and
;; a plucked-string figure. :note/:length trigger a synthesized instrument
;; instead of a WAV sample; :length is in steps. :instrument picks the voice
;; — "bass" (sawtooth, punchy stab, the default when omitted), "brass"
;; (sawtooth, slower swell attack/longer release), or "pluck" (Karplus-
;; Strong string synthesis). Try it: `basso play patterns/bass-groove.fnl`.

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
   {:step 12 :note "G2" :length 4}
   {:step 0 :note "C4" :length 8 :instrument "brass" :velocity 0.5}
   {:step 8 :note "Eb4" :length 8 :instrument "brass" :velocity 0.5}
   {:step 2 :note "G3" :length 2 :instrument "pluck" :velocity 0.7}
   {:step 6 :note "C4" :length 2 :instrument "pluck" :velocity 0.7}
   {:step 10 :note "Eb4" :length 2 :instrument "pluck" :velocity 0.7}
   {:step 14 :note "G4" :length 2 :instrument "pluck" :velocity 0.7}])
