;; shuffle-groove.fnl — a swung groove written in the short notation.
;; (swing 50) is straight; 62 is a lazy shuffle; 66 is a triplet feel.
;; In seq: x hits, o is a soft ghost hit, . or - rests, spaces and | are
;; ignored. In melody: a note name plays, _ holds it, . rests.

(bpm 96)
(steps 16)
(swing 62)

(fn pattern [bar]
  (let [fill? (= (% bar 4) 3)]
    (stack
      (seq "kick2.wav"    "x... ..x. x... ...." {:pan 0})
      (seq "snare.wav"    (if fill? "o... x..o x.oo xxxx" "o... x..o .o.. x...") {:pan 0})
      (seq "cl_hihat.wav" "x.xo x.xo x.xo x.x." {:pan 0.3 :velocity 0.6})
      (seq "open_hh.wav"  ".... .... .... ..x." {:pan 0.3 :velocity 0.5})
      (melody "bass"      "C2 _ . . . . Eb2 _ C2 _ . . . . G1 _"))))

pattern
