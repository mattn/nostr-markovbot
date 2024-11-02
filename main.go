package main

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/ikawaha/kagome-dict/uni"
	"github.com/ikawaha/kagome/v2/tokenizer"
	markov "github.com/mattn/go-markov"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

const name = "nostr-markovbot"

const version = "0.0.18"

var revision = "HEAD"

var (
	reJapanese = regexp.MustCompile(`[０-９Ａ-Ｚａ-ｚぁ-ゖァ-ヾ一-鶴]`)

	postRelays = []string{
		"wss://nostr-relay.nokotaro.com",
		"wss://relay-jp.nostr.wirednet.jp",
		"wss://nostr.holybea.com",
		"wss://relay.snort.social",
		"wss://relay.damus.io",
		"wss://relay.nostrich.land",
		"wss://nostr.h3z.jp",
	}

	ignores = []string{}
)

func contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}

var ngWords *regexp.Regexp

func init() {
	words := os.Getenv("MARKOVBOT_NGWORDS")
	if words != "" {
		ngWords = regexp.MustCompile(words)
	}
}

func hasNgWords(line string) bool {
	if ngWords != nil {
		if ngWords.MatchString(line) {
			return true
		}
	}
	return false
}

func run(dryrun bool, ignoresFile string, word string) error {
	length := -1

	nsec := os.Getenv("MARKOVBOT_NSEC")
	if nsec == "" {
		log.Fatal("MARKOVBOT_NSEC is required")
	}

	// load ignores.txt
	f, err := os.Open(ignoresFile)
	if err == nil {
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			text := scanner.Text()
			if strings.HasPrefix(text, "#") {
				continue
			}
			tok := strings.Split(text, " ")
			if len(tok) >= 1 {
				ignores = append(ignores, tok[0])
			}
		}
	}
	filter := nostr.Filter{
		Kinds: []int{nostr.KindTextNote},
		Limit: 500,
	}
	relay, err := nostr.RelayConnect(context.Background(), "wss://relay-jp.nostr.wirednet.jp/")
	if err != nil {
		return err
	}
	defer relay.Close()

	evs, err := relay.QuerySync(context.Background(), filter)
	if err != nil {
		return err
	}

	reURL, err := regexp.Compile(`\bhttps?://[^?\s]+`)
	if err != nil {
		return err
	}
	imageSuffixes := []string{
		".png",
		".jpeg",
		".jpg",
		".gif",
		".bmp",
	}

	m := markov.New()
	for _, ev := range evs {
		if isIgnoreNpub(ev.PubKey) {
			continue
		}

		for _, line := range strings.Split(ev.Content, "\n") {
			if !reJapanese.MatchString(line) {
				continue
			}
			if hasNgWords(line) {
				continue
			}

			found := false
		ignore:
			for _, u := range reURL.FindAllString(line, -1) {
				for _, suffix := range imageSuffixes {
					if strings.HasSuffix(u, suffix) {
						found = true
						break ignore
					}
				}
			}
			if found {
				continue
			}

			log.Println(ev.PubKey, line)
			m.Update(strings.TrimSpace(line))
			found = true
		}
	}

	t, err := tokenizer.New(uni.Dict(), tokenizer.OmitBosEos())
	if err != nil {
		log.Fatal(err)
	}

	bad := []string{
		"助詞",
		"補助記号",
	}
	var result string
	var limit int
	for {
		if limit++; limit > 500 {
			return errors.New("retry max")
		}
		var first string
		if word != "" {
			first = word
			word = ""
		} else {
			for {
				if limit++; limit > 500 {
					return errors.New("retry max")
				}
				first = m.First()
				tokens := t.Tokenize(first)
				if !contains(bad, tokens[0].Features()[0]) {
					break
				}
			}
		}

		result = strings.TrimSpace(m.Chain(first))
		if result != "" && (length == -1 || len([]rune(result)) <= length) {
			break
		}
	}

	if dryrun {
		fmt.Println(result)
		return nil
	}

	var sk string
	var pub string
	if _, s, err := nip19.Decode(nsec); err != nil {
		return err
	} else {
		sk = s.(string)
	}
	if pub, err = nostr.GetPublicKey(sk); err != nil {
		return err
	}

	var ev nostr.Event
	ev.PubKey = pub
	ev.Content = result
	ev.Tags = nostr.Tags{}
	ev.CreatedAt = nostr.Now()
	ev.Kind = nostr.KindTextNote
	if err := ev.Sign(sk); err != nil {
		return err
	}
	for _, r := range postRelays {
		relay, err := nostr.RelayConnect(context.Background(), r)
		if err != nil {
			log.Println(err)
			continue
		}
		fmt.Println(relay.Publish(context.Background(), ev))
		relay.Close()
	}
	return nil
}

func isIgnoreNpub(pub string) bool {
	npub, err := nip19.EncodePublicKey(pub)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(ignores, func(is string) bool {
		return is == npub
	})
}

func env(name string, def string) string {
	if val := os.Getenv(name); val != "" {
		return val
	}
	return def
}

func main() {
	var ver bool
	var ignoresFile string
	var dryrun bool
	flag.BoolVar(&ver, "version", false, "show version")
	flag.StringVar(&ignoresFile, "ignores", env("IGNORES", "ignores.txt"), "path to ignores.txt")
	flag.BoolVar(&dryrun, "dryrun", false, "dryrun")
	flag.Parse()

	if ver {
		fmt.Println(version)
		os.Exit(0)
	}

	if err := run(dryrun, ignoresFile, flag.Arg(0)); err != nil {
		log.Fatal(err)
	}
}
