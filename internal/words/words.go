// Package words holds the word lists of live-mode codes and request fingerprints
// (PROTOCOL.md sections 4 and 5.1): word k of the Turkish list means word k of the English
// one. It is the same list as web/src/lib/words.ts; a test keeps the two equal.
package words

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var pairs = [256][2]string{
	{"elma", "apple"}, {"armut", "pear"}, {"kiraz", "cherry"}, {"limon", "lemon"},
	{"muz", "banana"}, {"erik", "plum"}, {"incir", "fig"}, {"portakal", "orange"},
	{"kavun", "melon"}, {"ceviz", "walnut"}, {"badem", "almond"}, {"zeytin", "olive"},
	{"domates", "tomato"}, {"biber", "pepper"}, {"patates", "potato"}, {"lahana", "cabbage"},
	{"marul", "lettuce"}, {"fasulye", "bean"}, {"mantar", "mushroom"}, {"ekmek", "bread"},
	{"peynir", "cheese"}, {"bal", "honey"}, {"tuz", "salt"}, {"kahve", "coffee"},
	{"su", "water"}, {"yumurta", "egg"}, {"simit", "bagel"}, {"tavuk", "chicken"},
	{"kuzu", "lamb"}, {"kedi", "cat"}, {"at", "horse"}, {"inek", "cow"},
	{"koyun", "sheep"}, {"aslan", "lion"}, {"kaplan", "tiger"}, {"fil", "elephant"},
	{"zebra", "zebra"}, {"maymun", "monkey"}, {"tilki", "fox"}, {"kurt", "wolf"},
	{"geyik", "deer"}, {"sincap", "squirrel"}, {"kartal", "eagle"}, {"karga", "crow"},
	{"leylek", "stork"}, {"kaz", "goose"}, {"yunus", "dolphin"}, {"balina", "whale"},
	{"ahtapot", "octopus"}, {"kelebek", "butterfly"}, {"timsah", "crocodile"}, {"penguen", "penguin"},
	{"deve", "camel"}, {"panda", "panda"}, {"kanguru", "kangaroo"}, {"horoz", "rooster"},
	{"deniz", "sea"}, {"nehir", "river"}, {"dere", "stream"}, {"orman", "forest"},
	{"tepe", "hill"}, {"vadi", "valley"}, {"ada", "island"}, {"kum", "sand"},
	{"kaya", "rock"}, {"toprak", "soil"}, {"bulut", "cloud"}, {"kar", "snow"},
	{"ay", "moon"}, {"gezegen", "planet"}, {"sabah", "morning"}, {"gece", "night"},
	{"yaz", "summer"}, {"bahar", "spring"}, {"sonbahar", "autumn"}, {"buz", "ice"},
	{"duman", "smoke"}, {"demir", "iron"}, {"elmas", "diamond"}, {"inci", "pearl"},
	{"kristal", "crystal"}, {"cam", "glass"}, {"kalem", "pen"}, {"kitap", "book"},
	{"defter", "notebook"}, {"masa", "table"}, {"sandalye", "chair"}, {"pencere", "window"},
	{"duvar", "wall"}, {"merdiven", "ladder"}, {"anahtar", "key"}, {"kilit", "lock"},
	{"lamba", "lamp"}, {"mum", "candle"}, {"saat", "clock"}, {"ayna", "mirror"},
	{"yatak", "bed"}, {"battaniye", "blanket"}, {"perde", "curtain"}, {"dolap", "closet"},
	{"kutu", "box"}, {"sepet", "basket"}, {"valiz", "suitcase"}, {"bardak", "mug"},
	{"tabak", "plate"}, {"tava", "pan"}, {"tencere", "pot"}, {"kova", "bucket"},
	{"makas", "scissors"}, {"ip", "rope"}, {"kemer", "belt"}, {"etek", "skirt"},
	{"ceket", "jacket"}, {"palto", "coat"}, {"eldiven", "glove"}, {"terlik", "slipper"},
	{"bere", "beanie"}, {"yelek", "vest"}, {"kolye", "necklace"}, {"bilezik", "bracelet"},
	{"araba", "car"}, {"tren", "train"}, {"gemi", "ship"}, {"tekne", "boat"},
	{"bisiklet", "bicycle"}, {"roket", "rocket"}, {"kamyon", "truck"}, {"taksi", "taxi"},
	{"motor", "motor"}, {"tekerlek", "wheel"}, {"yol", "road"}, {"liman", "harbor"},
	{"istasyon", "station"}, {"sokak", "street"}, {"meydan", "plaza"}, {"park", "park"},
	{"tarla", "field"}, {"kale", "castle"}, {"kule", "tower"}, {"saray", "palace"},
	{"cami", "mosque"}, {"okul", "school"}, {"hastane", "hospital"}, {"market", "market"},
	{"banka", "bank"}, {"otel", "hotel"}, {"sinema", "cinema"}, {"tiyatro", "theater"},
	{"fabrika", "factory"}, {"ev", "home"}, {"oda", "room"}, {"mutfak", "kitchen"},
	{"banyo", "bathroom"}, {"balkon", "balcony"}, {"bayrak", "flag"}, {"harita", "map"},
	{"pusula", "compass"}, {"kamera", "camera"}, {"radyo", "radio"}, {"telefon", "phone"},
	{"piyano", "piano"}, {"gitar", "guitar"}, {"davul", "drum"}, {"keman", "violin"},
	{"zil", "bell"}, {"melodi", "melody"}, {"resim", "painting"}, {"heykel", "statue"},
	{"boya", "paint"}, {"tuval", "canvas"}, {"oyun", "game"}, {"top", "ball"},
	{"kart", "card"}, {"zar", "dice"}, {"bilmece", "riddle"}, {"masal", "tale"},
	{"hikaye", "story"}, {"mektup", "letter"}, {"zarf", "envelope"}, {"pul", "stamp"},
	{"posta", "mail"}, {"haber", "news"}, {"gazete", "newspaper"}, {"dergi", "magazine"},
	{"sayfa", "page"}, {"nokta", "dot"}, {"daire", "circle"}, {"kare", "square"},
	{"kalp", "heart"}, {"ok", "arrow"}, {"kalkan", "shield"}, {"yay", "bow"},
	{"hazine", "treasure"}, {"para", "money"}, {"fener", "lantern"}, {"kamp", "camp"},
	{"vida", "screw"}, {"tahta", "plank"}, {"beton", "concrete"}, {"kablo", "cable"},
	{"pil", "battery"}, {"kanal", "canal"}, {"baraj", "dam"}, {"kuyu", "well"},
	{"havuz", "pool"}, {"volkan", "volcano"}, {"buzul", "glacier"}, {"vaha", "oasis"},
	{"kanyon", "canyon"}, {"yaprak", "leaf"}, {"dal", "branch"}, {"tohum", "seed"},
	{"lale", "tulip"}, {"papatya", "daisy"}, {"zambak", "lily"}, {"yosun", "moss"},
	{"bambu", "bamboo"}, {"palmiye", "palm"}, {"kavak", "poplar"}, {"sedir", "cedar"},
	{"renk", "color"}, {"mavi", "blue"}, {"mor", "purple"}, {"pembe", "pink"},
	{"lacivert", "navy"}, {"beyaz", "white"}, {"siyah", "black"}, {"gri", "gray"},
	{"bordo", "maroon"}, {"bej", "beige"}, {"ritim", "rhythm"}, {"dans", "dance"},
	{"bayram", "holiday"}, {"parti", "party"}, {"hediye", "gift"}, {"pasta", "cake"},
	{"kurabiye", "cookie"}, {"pizza", "pizza"}, {"salata", "salad"}, {"sos", "sauce"},
	{"limonata", "lemonade"}, {"robot", "robot"}, {"sihir", "magic"}, {"hayal", "dream"},
	{"umut", "hope"}, {"cesaret", "courage"}, {"huzur", "peace"}, {"dost", "friend"},
	{"kahraman", "hero"}, {"korsan", "pirate"}, {"kaptan", "captain"}, {"pilot", "pilot"},
	{"doktor", "doctor"}, {"terzi", "tailor"}, {"ressam", "artist"}, {"bekci", "guard"},
}

var position = func() map[string]int {
	m := map[string]int{}
	for i, p := range pairs {
		m[p[0]], m[p[1]] = i, i
	}
	return m
}()

// list picks the column of a language: Turkish for "tr", English otherwise.
func list(lang string) int {
	if lang == "tr" {
		return 0
	}
	return 1
}

// Code is a live-mode code: the channel and two word positions.
type Code struct {
	Channel   int
	Positions [2]int
}

func (c Code) Format(lang string) string {
	l := list(lang)
	return fmt.Sprintf("%d-%s-%s", c.Channel, pairs[c.Positions[0]][l], pairs[c.Positions[1]][l])
}

var separators = regexp.MustCompile(`[\s.\-_]+`)

// Parse reads a code as people type it: any case, dashes, spaces or dots between the parts,
// and each word in Turkish or English.
func Parse(text string) (Code, error) {
	parts := separators.Split(strings.ToLower(strings.TrimSpace(text)), -1)
	if len(parts) != 3 || parts[0] == "" {
		return Code{}, errors.New("a code has three parts: a number and two words, like 7-apple-tower")
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil || n < 1 || len(parts[0]) > 6 {
		return Code{}, errors.New("the number at the start of the code can't be read")
	}
	c := Code{Channel: n}
	for i, w := range parts[1:] {
		p, ok := position[w]
		if !ok {
			return Code{}, fmt.Errorf("%q isn't in the word list; check the code", w)
		}
		c.Positions[i] = p
	}
	return c, nil
}

// Fingerprint is five words from the first five bytes of SHA-256(public key).
func Fingerprint(publicKey []byte, lang string) string {
	sum := sha256.Sum256(publicKey)
	l := list(lang)
	w := make([]string, 5)
	for i := range w {
		w[i] = pairs[sum[i]][l]
	}
	return strings.Join(w, " ")
}
