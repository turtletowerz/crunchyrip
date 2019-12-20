package main

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/turtletowerz/m3u8"
	"github.com/schollz/progressbar"
)

const (
	allowOutput     bool   = true
	defaultChannels int    = 25
	aesMethod       string = "AES-128"
)

type downloader struct {
	lock         sync.Mutex
	segments     []*m3u8.MediaSegment
	segmentCount int
	completed    int
	filename     string
	channelCount int
	client       *httpClient
	progress     *progressbar.ProgressBar
}

func writeOutput(format string, a ...interface{}) {
	if allowOutput {
		fmt.Printf(format+"\n", a...)
	}
}

func getKey(client *httpClient, baseURL *url.URL, keyPath string) (string, error) {
	var keyURL string

	if strings.HasPrefix(keyPath, "http") {
		keyURL = keyPath
	} else {
		result, err := baseURL.Parse(keyPath)
		if err != nil {
			return "", fmt.Errorf("parsing new key url: %w", err)
		}
		keyURL = result.String()
	}

	resp, err := client.Get(keyURL)
	if err != nil {
		return "", fmt.Errorf("getting key url: %w", err)
	}

	defer resp.Body.Close()
	keyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("getting key url: %w", err)
	}
	return string(keyBytes), nil
}

func newDownloader(client *httpClient, name, m3u8URL string, channels int) (*downloader, error) {
	parsedURL, err := url.Parse(m3u8URL)
	if err != nil {
		return nil, fmt.Errorf("parsing m3u8 url: %w", err)
	}

	resp, err := client.Get(parsedURL.String())
	if err != nil {
		return nil, fmt.Errorf("getting m3u8 url: %w", err)
	}

	defer resp.Body.Close()
	playlist, listType, err := m3u8.DecodeFrom(resp.Body, true)
	if err != nil {
		return nil, fmt.Errorf("parsing m3u8 url: %w", err)
	}

	if listType != m3u8.MEDIA {
		return nil, fmt.Errorf("bad list type (must be MEDIA)")
	}

	mediaPlaylist := playlist.(*m3u8.MediaPlaylist)
	segCount := int(mediaPlaylist.Count())
	mediaSegments := make([]*m3u8.MediaSegment, segCount)

	if mediaPlaylist.Key != nil && mediaPlaylist.Key.Method == aesMethod {
		keyString, err := getKey(client, parsedURL, mediaPlaylist.Key.URI)
		if err != nil {
			return nil, fmt.Errorf("getting playlist key: %w", err)
		}
		mediaPlaylist.Key.URI = keyString
	}

	for i, segment := range mediaPlaylist.Segments {
		if i == segCount {
			break
		}

		if strings.HasPrefix(segment.URI, "http") == false {
			newSegURL, err := parsedURL.Parse(segment.URI)
			if err != nil {
				return nil, fmt.Errorf("parsing segment uri: %w", err)
			}
			segment.URI = newSegURL.String()
		}

		if segment.Key != nil && segment.Key.Method == aesMethod {
			segKey, err := getKey(client, parsedURL, segment.Key.URI)
			if err != nil {
				return nil, fmt.Errorf("getting segment %d key: %w", segment.SeqId, err)
			}
			segment.Key.URI = segKey
		} else if mediaPlaylist.Key != nil && mediaPlaylist.Key.Method == aesMethod {
			segment.Key = mediaPlaylist.Key
		}
		mediaSegments[i] = segment
	}

	download := &downloader{
		segmentCount: segCount,
		segments:     mediaSegments,
		filename:     name,
		channelCount: channels,
		client:       client,
		progress:     progressbar.New(segCount),
	}
	return download, nil
}

func (d *downloader) Download(makeMP4 bool) error {
	os.RemoveAll(tsStorage)
	os.Mkdir(tsStorage, os.ModePerm)
	defer os.RemoveAll(tsStorage)

	var wg sync.WaitGroup

	for i := 0; i < d.channelCount; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				segment, done := d.next()

				if done {
					break
				}

				if err := d.downloadSegment(i, segment); err != nil {
					writeOutput("failed to download %d (will return to queue): %v", segment.SeqId, err)
					d.lock.Lock()
					d.segments = append(d.segments, segment)
					d.lock.Unlock()
				}
			}
		}()
	}

	wg.Wait()
	if err := d.merge(); err != nil {
		return fmt.Errorf("merging files: %w", err)
	}

	if makeMP4 {
		if err := d.toMP4(); err != nil {
			return fmt.Errorf("converting to mp4: %w", err)
		}
	}
	return nil
}

func (d *downloader) next() (*m3u8.MediaSegment, bool) {
	d.lock.Lock()
	defer d.lock.Unlock()

	if len(d.segments) == 0 {
		return nil, true
	}
	segment := d.segments[0]
	d.segments = d.segments[1:]
	return segment, false
}

func (d *downloader) merge() error {
	file, err := os.Create(tempDir + pathSep + d.filename + ".ts")
	if err != nil {
		return fmt.Errorf("creating final file: %w", err)
	}

	defer file.Close()
	writer := bufio.NewWriter(file)

	for i := 0; i < d.segmentCount; i++ {
		fileBytes, err := ioutil.ReadFile(tsStorage + string(os.PathSeparator) + strconv.Itoa(i) + ".ts")
		if err != nil {
			if err, ok := err.(*os.PathError); ok == false {
				writeOutput("Error reading bytes from %d: %v", i, err)
			}
			continue
		}

		_, err = writer.Write(fileBytes)
		if err != nil {
			writeOutput("Error writing bytes from %d: %v", i, err)
			continue
		}
	}
	writer.Flush()
	return nil
}

func findAbsoluteBinary(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		path = name
	}
	path, err = filepath.Abs(path)
	if err != nil {
		path = name
	}
	return path
}

func (d *downloader) toMP4() error {
	writeOutput("\nConverting %q to %q", d.filename+".ts", d.filename+".mp4")
	cmd := exec.Command(
		findAbsoluteBinary("ffmpeg"),
		"-i", d.filename+".ts",
		"-map", "0",
		"-c:v", "copy",
		"-c:a", "copy",
		"-metadata", `encoding_tool="no_variable_data"`,
		"-y", d.filename+".mp4",
	)
	cmd.Dir = tempDir

	if byteResult, err := cmd.Output(); err != nil {
		return fmt.Errorf("running ts to mp4: %w - result output: %s", err, string(byteResult))
	}
	os.Remove(tempDir + pathSep + d.filename + ".ts")
	return nil
}

func (d *downloader) downloadSegment(id int, segment *m3u8.MediaSegment) error {
	resp, err := d.client.Get(segment.URI)
	if err != nil {
		return fmt.Errorf("getting segment response: %w", err)
	}

	file, err := os.Create(tsStorage + string(os.PathSeparator) + strconv.FormatUint(segment.SeqId, 10) + ".ts")
	if err != nil {
		return fmt.Errorf("creating ts file: %w", err)
	}

	defer resp.Body.Close()
	defer file.Close()

	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading segment response: %w", err)
	}

	// This method and shorter and looks like it works fine If this ever fails...
	// https://github.com/Greyh4t/m3u8-Downloader-Go/blob/master/main.go#L214
	if segment.Key != nil && segment.Key.Method == aesMethod {
		aesBlock, err := aes.NewCipher([]byte(segment.Key.URI))
		if err != nil {
			return fmt.Errorf("creating aes cipher: %w", err)
		}

		iv := []byte(segment.Key.IV)
		if len(iv) == 0 {
			iv = []byte(segment.Key.URI)
		}
		blockMode := cipher.NewCBCDecrypter(aesBlock, iv[:aesBlock.BlockSize()])
		origData := make([]byte, len(respBytes))
		blockMode.CryptBlocks(origData, respBytes)

		length := len(origData)
		respBytes = origData[:(length - int(origData[length-1]))]
	}

	// Credits to github.com/oopsguy/m3u8 for this
	// https://en.wikipedia.org/wiki/MPEG_transport_stream
	// Some TS files do not start with SyncByte 0x47, they can not be played after merging,
	// Need to remove the bytes before the SyncByte 0x47(71).
	syncByte := uint8(71) //0x47
	for j := 0; j < len(respBytes); j++ {
		if respBytes[j] == syncByte {
			respBytes = respBytes[j:]
			break
		}
	}

	w := bufio.NewWriter(file)
	if _, err := w.Write(respBytes); err != nil {
		return fmt.Errorf("writing bytes to file: %w", err)
	}

	d.completed = d.completed + 1
	d.progress.Add(1)
	return nil
}

func getAccurateQuality(variants []*m3u8.Variant, quality string) ([]string, *m3u8.Variant) {
	qualities := map[int]*m3u8.Variant{}

	for _, val := range variants {
		res, exists := qualities[val.Resolution.Width]
		if (exists == false || (exists == true && val.Bandwidth > res.Bandwidth)) {
			qualities[val.Resolution.Width] = val
		}
	}

	var qualityStrings []string
	var bestQualityIndex int

	for width, variant := range qualities {
		resolutionString := fmt.Sprintf("%dx%d", variant.Resolution.Width, variant.Resolution.Height)
		qualityStrings = append(qualityStrings, resolutionString)

		if (quality == "max" && width > bestQualityIndex) || (quality == "min" && (width < bestQualityIndex || bestQualityIndex == 0)) || quality == resolutionString {
			bestQualityIndex = width
		}
	}

	if bestVariant, exists := qualities[bestQualityIndex]; exists == true {
		return qualityStrings, bestVariant
	}
	return qualityStrings, nil
}

func bestMasterStream(client *httpClient, url, quality string) (*m3u8.Variant, error) {
	resp, err := client.Get(url) 
	if err != nil {
		return nil, fmt.Errorf("getting video url: %w", err)
	}

	defer resp.Body.Close()
	playlist, listType, err := m3u8.DecodeFrom(resp.Body, true)
	if err != nil {
		return nil, fmt.Errorf("decoding m3u8 response: %w", err)
	}

	if listType == m3u8.MASTER {
		qualities, bestQuality := getAccurateQuality(playlist.(*m3u8.MasterPlaylist).Variants, quality)
		logInfo("Avaliable qualities: %s", strings.Join(qualities, ", "))

		if bestQuality == nil {
			return nil, fmt.Errorf("no stream of quality %q\nAvaliable qualities: %s", quality, strings.Join(qualities, ", "))
		}
		return bestQuality, nil
	}
	return nil, fmt.Errorf("not a master playlist")
}
