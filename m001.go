package main

import (
    "time"
)

var (
    pattern    = []string{
        kick2,
        maracas,
        cl_hihat,
        maracas,
        snare,
        maracas,
        cl_hihat,
        kick2,
        maracas,
        maracas,
        hightom,
        maracas,
        snare,
        kick1,
        cl_hihat,
        maracas,
    }
)

func main() {
    bpm := 120
    loops := 16
    step := time.Minute / time.Duration(bpm*4)

    play(pattern, loops, step)
}
