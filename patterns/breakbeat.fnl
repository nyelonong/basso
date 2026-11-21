;; breakbeat.fnl — a chopped break at 136 BPM in E minor, 8-bar loop.
;; An Amen-style kick and snare with ghost notes and a light swing, a rolling
;; saw bass, a held pad chord per bar, and a chopped fill every fourth bar.

(bpm 136)
(steps 16)
(swing 54)

(local chords [["E3" "G3" "B3"] ["E3" "G3" "B3"] ["C3" "E3" "G3"] ["D3" "F#3" "A3"]])
(local basslines ["E1 _ _ . E1 _ . G1 _ . A1 _ _ . B1 ."
                  "E1 _ _ . E1 _ . G1 _ . A1 _ _ . G1 ."
                  "C2 _ _ . C2 _ . E2 _ . G1 _ _ . C2 ."
                  "D2 _ _ . D2 _ . F#1 _ . A1 _ _ . D2 ."])

(fn held [note]
  (.. note (string.rep " _" 15)))

(fn pad [notes]
  (let [hits []]
    (each [_ note (ipairs notes)]
      (each [_ hit (ipairs (melody "pad" (held note) {:velocity 0.22}))]
        (table.insert hits hit)))
    hits))

(fn pattern [bar]
  (let [chord (+ 1 (% bar 4))
        fill? (= (% bar 4) 3)]
    (stack
      (seq "kick2.wav"      (if fill? "x... ..x. x... ...." "x.x. .... ..xx ....") {:pan 0})
      (seq "snare_hard.wav" (if fill? ".xox .xox o.xx xoxx" ".... x..o .o.. x..o") {:pan 0 :velocity 0.85})
      (seq "cl_hihat.wav"   "x.x. x.x. x.x. x.x." {:pan 0.25 :velocity 0.7})
      (seq "maracas.wav"    "oxox oxox oxox oxox" {:pan -0.3 :velocity 0.45})
      (seq "open_hh.wav"    ".... .... .... ..x." {:pan 0.25 :velocity 0.4})
      (when (= (% bar 8) 0) (seq "crashcym.wav" "x" {:pan 0 :velocity 0.6}))
      (when (= (% bar 8) 7) (seq "rev_cymbal.wav" "x" {:pan 0 :velocity 0.45}))
      (when fill? (seq "stab.wav" "..x. ..x. .... ...." {:pan 0 :velocity 0.4}))
      (melody "bass" (. basslines chord) {:velocity 0.9})
      (pad (. chords chord)))))

pattern
