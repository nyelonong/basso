(bpm 130)
(steps 16)

(fn hit [step sample velocity pan]
  {:step step :sample sample :velocity velocity :pan pan})

(fn pattern [bar]
  [; low conga backbone: tamborzao 3+3+2, twice per bar
   (hit 0 "conga1.wav" 1.0 -0.1)
   (hit 3 "conga1.wav" 0.85 -0.1)
   (hit 6 "conga1.wav" 0.8 -0.1)
   (hit 8 "conga1.wav" 0.95 -0.1)
   (hit 11 "conga1.wav" 0.85 -0.1)
   (hit 14 "conga1.wav" 0.8 -0.1)
   ; kick anchors under the conga pulse
   (hit 0 "kick2.wav" 0.9 0.0)
   (hit 8 "kick2.wav" 0.8 0.0)
   ; cowbell accents on the quarters
   (hit 0 "cowbell.wav" 0.7 0.3)
   (hit 4 "cowbell.wav" 0.55 0.3)
   (hit 8 "cowbell.wav" 0.7 0.3)
   (hit 12 "cowbell.wav" 0.55 0.3)
   ; driving hats, alternating pan
   (hit 0 "cl_hihat.wav" 0.5 -0.2)
   (hit 1 "cl_hihat.wav" 0.35 0.2)
   (hit 2 "cl_hihat.wav" 0.5 -0.2)
   (hit 3 "cl_hihat.wav" 0.35 0.2)
   (hit 4 "cl_hihat.wav" 0.5 -0.2)
   (hit 5 "cl_hihat.wav" 0.35 0.2)
   (hit 6 "cl_hihat.wav" 0.5 -0.2)
   (hit 7 "cl_hihat.wav" 0.35 0.2)
   (hit 8 "cl_hihat.wav" 0.5 -0.2)
   (hit 9 "cl_hihat.wav" 0.35 0.2)
   ; open hat lift mid-bar
   (hit 10 "open_hh.wav" 0.4 0.25)
   (hit 10 "cl_hihat.wav" 0.5 -0.2)
   (hit 11 "cl_hihat.wav" 0.35 0.2)
   (hit 12 "cl_hihat.wav" 0.5 -0.2)
   (hit 13 "cl_hihat.wav" 0.35 0.2)
   (hit 14 "cl_hihat.wav" 0.5 -0.2)
   (hit 15 "cl_hihat.wav" 0.35 0.2)
   ; snare roll filling the bar end, hotter every 4th bar
   (hit 12 "snare.wav" 0.55 0.1)
   (hit 13 "snare.wav" 0.65 0.1)
   (hit 14 "snare.wav" 0.75 0.1)
   (hit 15 "snare.wav" (if (= (% bar 4) 3) 1.0 0.9) 0.1)])

pattern
