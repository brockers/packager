package main

import "os"
import "os/exec"
import "flag"
import "time"
import "fmt"
import "regexp"
import "strings"
import "strconv"
import "encoding/json"
import "io/ioutil"
import "github.com/dghubble/oauth1"
// import "gopkg.in/russross/blackfriday.v2"
import "github.com/shurcooL/github_flavored_markdown"
import "github.com/microcosm-cc/bluemonday"

// Global checks
var is_crash bool = false

// Regular Expression filters
var authTag = regexp.MustCompile(`Author: .*\n`)
var dateTag = regexp.MustCompile(`Date: +([aAbcdDeFghiJlMnNoOprStTuvWy0-9\-\: ]{29,30})\n`)
var mer1Tag = regexp.MustCompile(`Merge pull request .*\n`)
var mer2Tag = regexp.MustCompile(`Merge: [a-f0-9]{7,9} [a-f0-9]{7,9}.*\n`)
var commNum = regexp.MustCompile(`^commit ([a-f0-9]{40,})(.*\n)`)
var hashLne = regexp.MustCompile(`^commit ([a-f0-9]{40,})`)

var leadSpace = regexp.MustCompile(`(?m)^[\t ]{2,}`)

type Response struct {
	Class    string `json:"ai_class"`
	PostType string `json:"type"`
	Commit   string `json:"commit"`
	Title    string `json:"title"`
	Message  string `json:"message"`
	Date     string `json:"timestamp"`
	Entered  int64  `json:"entered"`
	Disseminate PackageDisseminate `json:"package"`
}

type WpPost struct {
	Title    string `json:"title"`
	Status   string `json:"status"`
	Catagory int    `json:"categories"`
	Content  string `json:"content"`
	Comment  string `json:"comment_status"`
	Format   string `json:"format"`
	Media    string `json:"featured_media"`
}

type PackageDisseminate struct {
	Product string `json:"product"`
	Website string `json:"website"`
	Media   string `json:"media,omitempty"`
}

type PackageJSON struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	License     string `json:"license"`
	Private     bool   `json:"private"`
	Disseminate PackageDisseminate `json:"disseminate"`
}

// Standarize our error message
func check(e error, s string) {
	if e != nil {
		fmt.Println(s)
		panic(e)
	}
}

func checkEmpty(s string, v string) {
	if s == "" {
		panic("Empty value: " + v)
	}
}

func checkInital(is_valid bool, m string){
	if ! is_valid {
		if is_crash {
			panic(m)
		} else {
			fmt.Println(m)
			fmt.Println("Non Panic Exit.")
			os.Exit(0)
		}
	}
}


// Get the package file and convert it from json
func getPackage(f string) PackageJSON {

	raw, err := ioutil.ReadFile(f)
	if err != nil {
		panic("Unable to open configuration file: " + f )
	}

	var c PackageJSON
	if err := json.Unmarshal(raw, &c); err != nil {
		panic("Bad JSON stuff")
	}

	if c.Disseminate == (PackageDisseminate{}) {
		panic("No disseminate tag in configuration file. { disseminate: {} } is required.")
	}

	return c
}

// Log messages from git
func getGitlogMessage(n string) string {

	// exec.Command requires a single string for third arg.  Combine strings
	s := []string{ "git log -n", n }

	gitlogCmd := exec.Command("bash", "-c", strings.Join(s, "  "))
	gitlogOut, err := gitlogCmd.CombinedOutput()
	check(err, string(gitlogOut))

	m := string(gitlogOut)
	// First test is if the most recent update actually has a merge message
	if m == "" {
		panic("No message was obtained from git log")
	}

	return m
}

// Grab has from our commit message,
func getHashString(m string) string {

	var hsh []string
	hsh = hashLne.FindStringSubmatch(m)
	// fmt.Println("========================================================")
	// fmt.Println(hsh)

	// Store the message as a hsh document
	if len(hsh) < 2 || hsh[1] == "" {
		panic("Unable to get hash for git commit message. ")
	}

	if len(hsh[1]) != 40 {
		panic("SHA2 hashes cannot be longer than 40 characters.")
	}

	return hsh[1]
}

// Get time from commit message
func getCommitTime(m string) string {

	var cTime []string

	cTime = dateTag.FindStringSubmatch(m)

	// If we do not have one get very upset
	if len(cTime) < 2 || cTime[1] == "" {
		panic("Unable to get date/time for git commit message. ")
	}

	return cTime[1]
}

// Convert our json into an actual string
func (p PackageDisseminate) toString() string {
	return toJson(p)
}

// Decode json document into native go structure
func toJson(p interface{}) string {
	bytes, err := json.Marshal(p)
	check(err, "Failed to decide json document.")

	return string(bytes)
}

func cleanMessage(m string, min int, max int) string {

	m = authTag.ReplaceAllString(m, "")
	m = dateTag.ReplaceAllString(m, "")
	m = commNum.ReplaceAllString(m, "")
	m = mer1Tag.ReplaceAllString(m, "")
	m = mer2Tag.ReplaceAllString(m, "")
	m = strings.TrimSpace(m)
	m = leadSpace.ReplaceAllString(m, "")

	lenth := len(m)
	checkInital((lenth >= min), "Commit Message must meet minimum length. Length: " +
		strconv.Itoa(lenth) + " Min: " + strconv.Itoa(min))
	checkInital((lenth <= max), "Commit Message exceeds the maximum length. Length: " +
		strconv.Itoa(lenth) + " Max: " + strconv.Itoa(max))

	return m
}

func main(){

	// Message Conditions
	var maxLenth int = 1500
	var minLenth int = 180
	var is_all bool = false
	var is_print bool = false
	var is_markdown bool = false
	var configFile string = "./package.json"
	var saveFile string = "./disseminate.json"

	// Wordpress Post Options
	var is_post bool = false
	var wp_status string = "publish"
	var wp_category int = 11
	var wp_comment string = "closed"
	var wp_format string = "standard"

	// var number string
	var message string
	var hash string
	var commitTime string

	// oAuth1 values
	postClientKey := os.Getenv("D_OAUTH_CLIENT_KEY")
	postClientSecret := os.Getenv("D_OAUTH_CLIENT_SECRET")
	postToken := os.Getenv("D_OAUTH_TOKEN")
	postTokenSecret := os.Getenv("D_OAUTH_TOKEN_SECRET")
	postUrl := os.Getenv("D_POST_URL")

	// Setup OAuth1
	config := oauth1.NewConfig(postClientKey,postClientSecret)
	token := oauth1.NewToken(postToken,postTokenSecret)

	// httpClient will automatically authorize http.Request's
	httpClient := config.Client(oauth1.NoContext, token)

	// Build our timestamp
	now := time.Now()

	// Less simpler printing
	p := fmt.Println

	// Configuration
	configFilePtr := flag.String("config", configFile, "Disseminate configuration file in JSON format.")

	// Message Checks
	// TODO: These really should get set in the configuration file and only override here
	maxLenthPtr := flag.Int("max", maxLenth, "Maximum allowable length of the commit message.")
	minLenthPtr := flag.Int("min", minLenth, "Minimum allowable length of the commit message. 0 for no minimum.")

	// Output Options
	is_printPtr := flag.Bool("print", is_print, "Print output to terminal instead of to a file.")
	is_postPtr := flag.Bool("post", is_post, "Post to a RESTful Endpoint. You will need a number of ENV setup to do this.")
	saveFilePtr := flag.String("save", saveFile, "Save output to a file.  Cannot be used with -print.")
	is_markdownPtr := flag.Bool("markdown", is_markdown, "Support Markdown formatting in the gitlog message and convert to HTML for Wordpress.")
	is_allPtr := flag.Bool("all", is_all, "Include ALL commits in results. Default behavior is to only include merges.")

	// Process Management
	is_crashPtr := flag.Bool("term", is_crash, "Terminate disseminate (exit panic) if message is empty or under min length. Normally just warn.")

	flag.Parse()
	// Reset our defaults to new imports
	is_post = *is_postPtr
	minLenth = *minLenthPtr
	maxLenth = *maxLenthPtr
	is_print = *is_printPtr
	is_markdown = *is_markdownPtr
	is_crash = *is_crashPtr

	// Get out package information
	pkgs := getPackage(*configFilePtr)

	// Get our package messages
	message = getGitlogMessage("1")
	hash = getHashString(message)
	commitTime = getCommitTime(message)

	// If we have a merge requirement make sure we check for it
	if ! (*is_allPtr) {
		checkInital(mer2Tag.MatchString(message), "Commit must be a merge.")
	}

	// Now we remove the author, date, and commit from the message
	message = cleanMessage(message, minLenth, maxLenth)

	if is_markdown {
		// Convert Message from Markdown to HTML, then safety check the result
		// markdown := blackfriday.Run([]byte(message))
		markdown := github_flavored_markdown.Markdown([]byte(message))
		message = string(bluemonday.UGCPolicy().SanitizeBytes(markdown))
	}

	// TODO: Ultimately there need to be some natural language checks here
	title := pkgs.Disseminate.Product + " - Latest Updates"

	res := &Response{
		Commit: hash,
		PostType: "global",
		Title: title,
		Date: commitTime,
		Message: message,
		Entered: now.Unix() * 1000, // Unix time is second, need millisecons
		Class: "feed_global",
		Disseminate: pkgs.Disseminate}
	resJson, _ := json.Marshal(res)

	// Post to a RESTFUL API
	if is_post {

		// Better have something in there
		checkEmpty(postClientKey, "D_OAUTH_CLIENT_KEY")
		checkEmpty(postClientSecret, "D_OAUTH_CLIENT_SECRET")
		checkEmpty(postToken, "D_OAUTH_TOKEN")
		checkEmpty(postTokenSecret, "D_OAUTH_TOKEN_SECRET")
		checkEmpty(postUrl, "D_POST_URL")

		post := &WpPost{
			Title: title,
			Status: wp_status,
			Catagory:wp_category,
			Comment: wp_comment,
			Format: wp_format,
			Media: pkgs.Disseminate.Media,
			Content: message}
		postJson, _ := json.Marshal(post)

		resp, err := httpClient.Post(postUrl,  "application/json", strings.NewReader(string(postJson)))
		defer resp.Body.Close()

		// Check for bad post
		check(err, "Unexpected Error with HTTP POST request")

		if 	resp.StatusCode != 201 {
			body, _ := ioutil.ReadAll(resp.Body)
			panic(string(body))
		} else {
			p("Post Successful")
		}
	}

	// Print or write.
	if is_print {
		p(string(resJson))
	} else {
		err := ioutil.WriteFile(*saveFilePtr, resJson, 0644)
		check(err, "Unable to write to save file")
		p( "File been updated:", (*saveFilePtr) )
	}
}
