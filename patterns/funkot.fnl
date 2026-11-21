;; funkot: fast, straight, relentless. A hard driven kick on every beat, open
;; hats on the off-beats, kendang chatter, an octave-jumping off-beat bass,
;; and an eight-bar phrase with a siren on top and a reverse swell into it.

(bpm 180)
(steps 16)

(local roots [["A1" "A2"] ["A1" "A2"] ["F1" "F2"] ["G1" "G2"]])

(fn bassline [bar]
  (let [[lo hi] (. roots (+ 1 (% bar 4)))
        beat (.. ". . " lo " " hi " ")]
    (melody "bass" (.. beat beat beat beat) {:velocity 0.8})))

(fn pattern [bar]
  (let [phrase (% bar 8)
        fill? (= (% bar 4) 3)]
    (stack
      (seq "kick_hard.wav"    "x... x... x... x..." {:pan 0 :velocity 0.85})
      (seq "open_hh.wav"      "..x. ..x. ..x. ..x." {:pan 0.2 :velocity 0.6})
      (seq "cl_hihat.wav"     "xo.o xo.o xo.o xo.o" {:pan -0.2 :velocity 0.4})
      (seq "snare_hard.wav"   ".... x... .... x..." {:pan 0 :velocity 0.8})
      (seq "kendang_tak.wav"  "...x .x.. ...x .x.x" {:pan 0.35 :velocity 0.7})
      (seq "kendang_dung.wav" ".x.. ..x. .x.. ..x." {:pan -0.35 :velocity 0.8})
      (when fill?
        (seq "kendang_tung.wav" ".... .... xoxo xxxx" {:pan 0.2}))
      (when (= phrase 0)
        (stack (seq "crashcym.wav" "x" {:pan 0 :velocity 0.7})
               (seq "hoover.wav" "x" {:pan 0 :velocity 0.6})
               (seq "siren.wav" "x" {:pan 0 :velocity 0.35})))
      (when (= phrase 4) (seq "stab.wav" "x..x ..x." {:pan 0 :velocity 0.6}))
      (when (= phrase 7)
        (stack (seq "rev_cymbal.wav" "x" {:pan 0 :velocity 0.6})
               (seq "zap.wav" ".... .... .... ..xx" {:pan 0 :velocity 0.5})))
      (bassline bar)
      (melody "lead" "A4 . A4 . C5 . A4 . E5 . D5 . C5 . A4 ." {:velocity 0.3}))))

pattern
