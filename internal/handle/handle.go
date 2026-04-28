// Package handle generates the friendly adjective-animal names collab-ai
// uses for default peer handles ("clever-otter") and session IDs
// ("happy-panda"). Same generator, same shape — separate consumers.
//
// 60 adjectives × 60 animals = 3600 combos. Plenty of headroom for a
// hackathon-scale tool; collisions on a single machine are handled by a
// numeric suffix when needed.
package handle

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

var adjectives = []string{
	"ancient", "bold", "brave", "bright", "calm", "clever", "cosmic", "curious",
	"dapper", "deft", "eager", "electric", "elated", "fancy", "feisty", "fierce",
	"fluffy", "frosty", "gentle", "golden", "happy", "hidden", "humble", "jolly",
	"keen", "kind", "lively", "lucid", "lucky", "merry", "mighty", "neat",
	"nimble", "noble", "playful", "plucky", "polite", "proud", "quiet", "quick",
	"royal", "rustic", "savvy", "sharp", "silly", "silver", "sleek", "sleepy",
	"snappy", "spry", "sturdy", "sunny", "swift", "tidy", "vivid", "warm",
	"witty", "wild", "wise", "zesty",
}

var animals = []string{
	"axolotl", "badger", "bear", "beaver", "bee", "capybara", "caribou", "cheetah",
	"chinchilla", "crow", "dolphin", "ferret", "finch", "fox", "frog", "gecko",
	"giraffe", "goose", "hawk", "hedgehog", "heron", "honeybee", "ibex", "kestrel",
	"kingfisher", "koala", "lemur", "leopard", "lion", "lynx", "magpie", "marten",
	"mole", "moose", "narwhal", "newt", "octopus", "otter", "owl", "panda",
	"panther", "parrot", "pelican", "platypus", "porcupine", "puffin", "raccoon",
	"raven", "salamander", "sandpiper", "seahorse", "seal", "sloth", "sparrow",
	"squid", "starling", "swan", "tiger", "weasel", "wolf",
}

// New returns a random adjective-animal pair. Uses crypto/rand so different
// processes seeded near-simultaneously won't collide on the same name.
func New() string {
	return adjectives[idx(len(adjectives))] + "-" + animals[idx(len(animals))]
}

// NewUnique returns a name not already present in taken. If every random
// pick collides (very unlikely), a numeric suffix is appended after 10
// attempts.
func NewUnique(taken func(string) bool) string {
	for range 10 {
		n := New()
		if !taken(n) {
			return n
		}
	}
	for i := 2; ; i++ {
		n := fmt.Sprintf("%s-%d", New(), i)
		if !taken(n) {
			return n
		}
	}
}

func idx(n int) int {
	bi, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		// crypto/rand failure is exceptional; fall back to first index
		// rather than panic in a CLI tool.
		return 0
	}
	return int(bi.Int64())
}
