<a href="https://goreportcard.com/report/github.com/turtletowerz/crunchyrip"><img src="https://goreportcard.com/badge/github.com/turtletowerz/crunchyrip" alt="Go Report Card"></a>

**`crunchyrip`** is a Crunchyroll series/episode downloader loosely based off of anirip. It provides the same features as anirip while making small additions that improve the overall experience
#### Features
- Download individual episodes or entire series
- Specify quality and subtitle language with optional flags
- Custom HLS downloader providing faster download speeds
- Basic stack-trace for easily identifying errors

### Installation
	go get github.com/turtletowerz/crunchyrip

#### Usage
The command will download the series/episode into the current working directory
	
	crunchyrip [flags] username password series-url

#### Flag Options
These are **optional** flags that allow the user to specify small changes they would like with the download

- Quality (-quality, -q): 240, 360, 480, 720, 1080. If the previous quality options are not found on the video, you can specify a custom resolution by doing `-q [WidthxHeight]` ex. `-q 624x480` (default 720)
- Subtitles (-subs, -s): Any RFC 5646 language code (ex. en-US, ja-JP, es-MX). Note not all subtitle languages are supported, and a language code of `none` will ignore subtitles when downloading (default en-US)
- Dubbed (-dub): If true, will attempt to download the dubbed version of the series (default false)

### Examples
	crunchyrip username password https://www.crunchyroll.com/dr-stone

	crunchyrip -q 1080 username password https://www.crunchyroll.com/dr-stone

	crunchyrip -s ja-JP username password https://www.crunchyroll.com/dr-stone/episode-20-the-age-of-energy-789333


##### To-Do
- Add *print-subs* bool to allow users to see which subtitle languages can be used
- Clean up hls.go, as it's a bit of a mess right now