package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/gookit/color"
)

const (
	crunchyrollReg string = `https://www.crunchyroll.com/([a-z0-9-]+)/?([a-z0-9-]+)?`
	prefix         string = "[crunchyrip] "
	illegalChars   string = `[\\\\/:*?\"<>|]`
	pathSep        string = string(os.PathSeparator)
)

var (
	errOptions error  = fmt.Errorf("OPTIONS_ERROR")
	tempDir    string = os.TempDir() + string(os.PathSeparator) + "crunchyrip"
	tsStorage  string = tempDir + pathSep + "tsStorage"

	// I need to figure out the anime with the most seasons
	numbers = []string{"One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine", "Ten"}

	resolutionList = map[string]string{
		"240":  "428x240",
		"360":  "640x360",
		"480":  "848x480",
		"720":  "1280x720",
		"1080": "1920x1080",
	}
)

func logCyan(format string, a ...interface{}) {
	color.Cyan.Printf(prefix+format+"\n", a...)
}

func logInfo(format string, a ...interface{}) {
	color.White.Printf(prefix+format+"\n", a...)
}

func logSuccess(format string, a ...interface{}) {
	color.Green.Printf(prefix+format+"\n", a...)
}

func logError(err error) {
	color.Red.Println(prefix + "Error " + err.Error())
}

func renameFile(src, dst string) error {
	for i := 0; i < 10; i++ {
		if err := os.Rename(src, dst); err == nil {
			return nil
		}
	}
	return fmt.Errorf("changing filename")
}

func cleanFilename(name string) string {
	return regexp.MustCompile(illegalChars).ReplaceAllString(name, "")
}

func getSeason(season string) string {
	num, _ := strconv.Atoi(season)
	if num == 0 {
		return "Specials"
	}
	return "Season " + numbers[num-1]
}

func main() {
	rand.Seed(time.Now().UnixNano())
	options := flag.Bool("options", false, "If true, will print all available resolutions and subtitle languages for the series")
	dub := flag.Bool("dub", false, "If true, will attempt to download English version")
	subs := flag.String("subs", "en-US", "Subtitle language: en-US, ja-JP (default en-US)")
	flag.StringVar(subs, "s", *subs, "Subtitle language: en-US, ja-JP (default en-US) (shorthand)")
	quality := flag.String("quality", "720", "Stream quality (default 720)")
	flag.StringVar(quality, "q", *quality, "Stream quality (shorthand)")

	flag.Parse()
	if flag.NArg() < 3 {
		logInfo("Usage: crunchyrip [flags] username password series-url")
		os.Exit(1)
	}

	logCyan("crunchyrip v0.0.2 - by turtletowerz")
	logCyan("Attempting to login to crunchyroll account")
	logInfo("Logging into Crunchyroll...")
	crunchyrollClient := newHTTPClient()

	if err := crunchyrollClient.Login(flag.Arg(0), flag.Arg(1)); err != nil {
		logError(err)
		return
	}

	logSuccess("Crunchyroll login successful!")
	if err := download(crunchyrollClient, flag.Arg(2), *quality, *subs, *dub, *options); err != nil {
		logError(err)
	}
	return
}

func download(client *httpClient, showURL, quality, subLang string, dubbed, options bool) error {
	_, statErr := os.Stat(tempDir)
	if statErr != nil {
		logInfo("Generating new temporary directory")
		os.Mkdir(tempDir, os.ModePerm)
	}

	if dubbed {
		subLang = "none"
	}

	logInfo("Scraping show metadata...")
	episodes, err := getEpisodes(client, showURL, dubbed)
	if err != nil {
		return fmt.Errorf("getting episodes: %w", err)
	}

	if len(episodes) == 0 {
		return fmt.Errorf("No episodes found!")
	}

	singleEpisode := (len(episodes) == 1)

	for _, episode := range episodes {
		logInfo("Retrieving Episode Info...")
		if err := episode.GetEpisodeInfo(client, subLang); err != nil {
			logError(fmt.Errorf("getting episode info: %w", err))
			continue
		}

		filename := cleanFilename(fmt.Sprintf("%s - S%02sE%02s - %s.mp4", episode.SeriesTitle, episode.SeasonNumber, episode.Number, episode.Title))

		var filepath string
		if singleEpisode == false {
			filepath = cleanFilename(episode.SeriesTitle) + pathSep + getSeason(episode.SeasonNumber) + pathSep
			os.MkdirAll(filepath, os.ModePerm)
		}

		if _, err := os.Stat(filepath + filename); err == nil {
			logSuccess("%s.mkv has already been downloaded successfully!", episode.Title)
			continue
		}

		logCyan("Downloading: %s", episode.Title)
		if err := episode.Download(client, quality, options); err != nil {
			if err == errOptions {
				return nil
			}
			logError(fmt.Errorf("downloading episode: %w", err))
			continue
		}

		if err := renameFile(tempDir+pathSep+"episode.mp4", filepath+filename); err != nil {
			logError(fmt.Errorf("renaming file: %w", err))
			continue
		}
		logSuccess("Downloading completed successfully!")

	}
	logCyan("Completed downloading episode(s)!")
	logInfo("Cleaning up temporary directory...")
	os.RemoveAll(tempDir)
	return nil
}
