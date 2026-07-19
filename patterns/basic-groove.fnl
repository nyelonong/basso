;; basic-groove.fnl — a simple 4/4 kick/snare/hihat groove.
;; A good first pattern to try: `basso play patterns/basic-groove.fnl`.
;; Edit a :sample or :step below, save, and hear it change on the next bar.

(bpm 120)
(steps 16)

(fn pattern [bar]
  [{:step 0 :sample "kick2.wav"}
   {:step 2 :sample "cl_hihat.wav"}
   {:step 4 :sample "snare.wav"}
   {:step 6 :sample "cl_hihat.wav"}
   {:step 8 :sample "kick2.wav"}
   {:step 10 :sample "cl_hihat.wav"}
   {:step 12 :sample "snare.wav"}
   {:step 14 :sample "cl_hihat.wav"}])
