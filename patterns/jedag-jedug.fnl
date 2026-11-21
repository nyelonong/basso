;; jedag-jedug.fnl — Indo bounce in A minor, 16-bar loop.
;; Bars 0-3 are kick, hats and bass; bars 4-6 add the clap backbeat; bar 7
;; builds with a rising snare roll and a reverse swell; bars 8-14 drop the
;; bouncy lead, doubled by a pluck, over Am F C G with open hats, kendang and
;; shaker; bar 15 builds back to the top.

(bpm 135)
(steps 16)

(local roots ["A1" "F1" "C2" "G1"])
(local riffs ["A4 . . A4 . . C5 . A4 . . E5 . . D5 ."
              "A4 . . A4 . . C5 . A4 . . F5 . . E5 ."
              "G4 . . G4 . . C5 . G4 . . E5 . . D5 ."
              "G4 . . G4 . . B4 . G4 . . D5 . . B4 ."])

(fn rising-roll [sample]
  (fcollect [i 0 15]
    {:step i :sample sample :pan 0 :velocity (+ 0.25 (* 0.05 i))}))

(fn pattern [bar]
  (let [phrase (% bar 16)
        chord (+ 1 (% bar 4))
        root (. roots chord)
        build? (or (= phrase 7) (= phrase 15))
        drop? (and (>= phrase 8) (not build?))
        backbeat? (or drop? (>= phrase 4))]
    (stack
      (seq "kick_hard.wav" (if build? "x... x... x... x.xx" "x... x... x... x...") {:pan 0})
      (seq "cl_hihat.wav"  "xoxo xoxo xoxo xoxo" {:pan -0.2 :velocity (if drop? 0.6 0.45)})
      (melody "bass" (.. ". . " root " _ . . " root " _ . . " root " _ . . " root " _"))
      (when backbeat?
        (stack (seq "handclap.wav"   ".... x... .... x..." {:pan 0 :velocity 0.8})
               (seq "snare_hard.wav" ".... x... .... x..." {:pan 0 :velocity 0.5})))
      (when build?
        (stack (rising-roll "snare_hard.wav")
               (seq "rev_cymbal.wav" "x" {:pan 0 :velocity 0.7})))
      (when (= phrase 8)
        (stack (seq "crashcym.wav" "x" {:pan 0 :velocity 0.8})
               (seq "siren.wav" "x" {:pan 0 :velocity 0.3})
               (seq "zap.wav" "x" {:pan 0 :velocity 0.5})))
      (when drop?
        (stack (melody "lead" (. riffs chord) {:velocity 0.85})
               (melody "pluck" (. riffs chord) {:velocity 0.5})
               (seq "open_hh.wav" "..x. ..x. ..x. ..x." {:pan 0.2 :velocity 0.75})
               (seq "kendang_tak.wav" "...x .x.. ...x .x.x" {:pan 0.35 :velocity 0.6})
               (seq "maracas.wav" "xoxo xoxo xoxo xoxo" {:pan 0.4 :velocity 0.5}))))))

pattern
