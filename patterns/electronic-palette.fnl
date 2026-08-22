(bpm 124)
(steps 16)

(fn pattern [bar]
  (let [even-bar (= (% bar 2) 0)
        root (if even-bar "C3" "A2")
        third (if even-bar "E3" "C3")
        fifth (if even-bar "G3" "E3")
        lead-a (if even-bar "E4" "C4")
        lead-b (if even-bar "G4" "E4")]
    [{:step 0 :sample "kick2.wav" :velocity 0.9}
     {:step 8 :sample "kick2.wav" :velocity 0.8}
     {:step 4 :sample "snare.wav" :velocity 0.75}
     {:step 12 :sample "snare.wav" :velocity 0.75}
     {:step 2 :sample "cl_hihat.wav" :velocity 0.35 :pan -0.2}
     {:step 6 :sample "cl_hihat.wav" :velocity 0.35 :pan 0.2}
     {:step 10 :sample "cl_hihat.wav" :velocity 0.35 :pan -0.2}
     {:step 14 :sample "cl_hihat.wav" :velocity 0.35 :pan 0.2}
     {:step 0 :note root :instrument "pad" :length 16 :velocity 0.25}
     {:step 0 :note third :instrument "pad" :length 16 :velocity 0.25}
     {:step 0 :note fifth :instrument "pad" :length 16 :velocity 0.25}
     {:step 4 :note lead-a :instrument "lead" :length 2 :velocity 0.6 :pan -0.15}
     {:step 8 :note lead-b :instrument "lead" :length 2 :velocity 0.65 :pan 0.15}
     {:step 12 :note lead-a :instrument "lead" :length 2 :velocity 0.55 :pan -0.1}]))

pattern
