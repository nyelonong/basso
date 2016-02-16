package main

import (
    "fmt"
    "os"
    "math/rand"
    "time"

    "github.com/outrightmental/go-atomix"
    "github.com/outrightmental/go-atomix/bind"
)

// Assets
const (
    path string   = "sound/808/"
    kick1 string    = "kick1.wav"
    kick2 string    = "kick2.wav"
    conga string    = "conga1.wav"
    cowbell string  = "cowbell.wav"
    crashcym string = "crashcym.wav"
    handclap string = "handclap.wav"
    hi_conga string = "hi_conga.wav"
    hightom string  = "hightom.wav"
    maracas string  = "maracas.wav"
    open_hh string  = "open_hh.wav"
    rimshot string  = "rimshot.wav"
    snare string    = "snare.wav"
    tom1 string     = "tom1.wav"
    cl_hihat string = "cl_hihat.wav"
)

// Audio Stuff
const (
    sampleHz float64    = float64(48000)
)

func play(pattern []string, loops int, step time.Duration) {
    defer atomix.Teardown()

    spec := bind.AudioSpec{
        Freq:     sampleHz,
        Format:   bind.AudioF32,
        Channels: 2,
    }

    atomix.Configure(spec)
    atomix.SetSoundsPath(path)
    atomix.StartAt(time.Now().Add(1 * time.Second))

    t := 1 * time.Second // padding before music
    for n := 0; n < loops; n++ {
        for s := 0; s < len(pattern); s++ {
            atomix.SetFire(pattern[s], t+time.Duration(s)*step, 0, 1.0, rand.Float64() * 2 - 1)
        }
        t += time.Duration(len(pattern)) * step
    }

    atomix.OpenAudio()

    fmt.Printf("Atomix, pid:%v, spec:%v\n", os.Getpid(), spec)
    for atomix.FireCount() > 0 {
        time.Sleep(1 * time.Second)
    }
}
