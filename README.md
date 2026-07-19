# basso

A live-coding player: a persistent process that plays a Fennel pattern
continuously and reloads it at the next bar boundary when you save the
source file, with no audio restart.

### Install

    make install

Installs to `$(go env GOPATH)/bin/basso`. No other dependencies — pure Go,
no cgo, no native libraries.

### Play

    basso play patterns/basic-groove.fnl

Or without installing:

    make run FILE=patterns/basic-groove.fnl

Edit the `.fnl` file and save while it's playing — the change takes effect
at the next bar. Ctrl-C to stop.

### Develop

    make build   # bin/basso
    make test
    make vet
    make fmt     # gofmt -l .
    make gates   # fmt + vet + test
