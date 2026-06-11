package portalauth

import (
	"crypto/rand"
	"math/big"
	"strings"
)

// commonWords is a small list of simple, memorable English words for student passwords.
var commonWords = []string{
	"apple", "ball", "bear", "bird", "blue", "book", "cake", "cat", "cloud", "dog",
	"duck", "egg", "fish", "frog", "game", "green", "hat", "house", "king", "kite",
	"lamp", "leaf", "lion", "moon", "mouse", "orange", "pencil", "pizza", "plane", "queen",
	"rabbit", "rain", "red", "ring", "rocket", "rose", "ship", "shoe", "snow", "song",
	"star", "sun", "table", "tiger", "train", "tree", "water", "whale", "wind", "wolf",
	"yellow", "zebra", "bridge", "castle", "crown", "drum", "feather", "flower", "forest", "garden",
	"globe", "grape", "hammer", "island", "jacket", "jungle", "kitten", "ladder", "lemon", "magnet",
	"melon", "mountain", "needle", "ocean", "painter", "panda", "peach", "pear", "piano", "pillow",
	"planet", "pocket", "popcorn", "puppet", "puzzle", "river", "robot", "sandwich", "shield", "slide",
	"snail", "spider", "spoon", "square", "strawberry", "swing", "teapot", "tent", "ticket", "toast",
	"towel", "triangle", "trumpet", "turtle", "violin", "window", "wizard", "anchor", "basket", "butterfly",
}

// MemorablePassword generates a random password made of n words separated by hyphens.
func MemorablePassword(wordCount int) (string, error) {
	if wordCount < 1 {
		wordCount = 3
	}
	max := big.NewInt(int64(len(commonWords)))
	var words []string
	for i := 0; i < wordCount; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		words = append(words, commonWords[n.Int64()])
	}
	return strings.Join(words, "-"), nil
}

// RandomOrMemorablePassword generates either a random alphanumeric or memorable password.
func RandomOrMemorablePassword(memorable bool) (string, error) {
	if memorable {
		return MemorablePassword(3)
	}
	return RandomPassword(12)
}
