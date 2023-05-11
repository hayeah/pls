package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"text/template"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/atotto/clipboard"
	"github.com/sashabaranov/go-openai"

	"github.com/hayeah/pls/promptstr"
)

type Chat struct {
	client      *openai.Client
	baseRequest openai.ChatCompletionRequest
}

type ChatOptions func(*Chat)

func toMessages(role string, messages []string) []openai.ChatCompletionMessage {
	var result []openai.ChatCompletionMessage
	for _, message := range messages {
		result = append(result, openai.ChatCompletionMessage{
			Role:    role,
			Content: message,
		})
	}
	return result
}

func SetMaxTokens(maxTokens int) ChatOptions {
	return func(c *Chat) {
		c.baseRequest.MaxTokens = maxTokens
	}
}

// AppendUserMessages sets context messages
func AppendUserMessages(messages ...string) ChatOptions {
	return func(c *Chat) {
		c.baseRequest.Messages = append(c.baseRequest.Messages, toMessages(openai.ChatMessageRoleUser, messages)...)
	}
}

func NewChat(client *openai.Client, opts ...ChatOptions) *Chat {
	c := &Chat{
		client: client,
		baseRequest: openai.ChatCompletionRequest{
			// Temperature: 0.5,
			// Temperature: 1.5. seems bad
			// Model: openai.GPT3Dot5Turbo,
			// Model:     openai.GPT3Dot5Turbo0301,
			// MaxTokens: 1000,
			Model: openai.GPT3Dot5Turbo0301,
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func (c *Chat) cloneRequest() openai.ChatCompletionRequest {
	return c.baseRequest
}

func (rs *ResponseStream) Close() error {
	rs.cancel()
	rs.stream.Close()
	return nil
}

func (c *Chat) Stream(message string, opts *TemplateFrontMatter) (io.ReadCloser, error) {
	ctx, cancel := context.WithCancel(context.Background())

	req := c.cloneRequest()
	if opts != nil {
		req.Temperature = float32(opts.Temperature)
	}

	req.Messages = append(req.Messages,
		// openai.ChatCompletionMessage{
		// 	Role:    openai.ChatMessageRoleSystem,
		// 	Content: "please be as helpful as possible, and give detailed, informative response. it's good to produce long output to be extra helpful.",
		// },
		openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: message,
		})
	req.Stream = true

	stream, err := c.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		cancel()
		return nil, err
	}

	rs := &ResponseStream{
		stream: stream,
		cancel: cancel,
	}

	return rs, nil
}

type ResponseStream struct {
	stream *openai.ChatCompletionStream
	cancel context.CancelFunc

	stopped bool
}

// Read streams the completion stream, and append a newline at the end. Not threadsafe.
func (rs *ResponseStream) Read(p []byte) (int, error) {
	if rs.stopped {
		return 0, io.EOF
	}

	// the base stream is not threadsafe...
	response, err := rs.stream.Recv()

	if errors.Is(err, io.EOF) {
		p[0] = '\n'
		rs.stopped = true
		return 1, io.EOF
	}

	if err != nil {
		return 0, err
	}

	n := copy(p, response.Choices[0].Delta.Content)
	return n, nil
}

type TemplateData struct {
	Input string
}

type TemplateFrontMatter struct {
	// note: quirk of the openai library doesn't make it possible to use 0.0 for these options floats.
	Temperature float32 `json:"temperature"`
}

func RenderTemplate(promptTemplate string, data TemplateData) (string, *TemplateFrontMatter, error) {
	// this is my prompt yo
	// ---
	// END_OF_PROMPT. BEGIN INPUT.
	// ---
	// {{.Input}}`
	var fm TemplateFrontMatter
	promptBody, err := promptstr.ParseFrontMatter(promptTemplate, &fm)
	if err != nil {
		return "", nil, err
	}

	tmpl, err := template.New("template").Parse(promptBody)

	if err != nil {
		return "", nil, err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		return "", nil, err
	}

	return buf.String(), &fm, nil
}

type Args struct {
	PromptFile string `arg:"positional,required" help:"prompt template file"`
	InputFile  string `arg:"positional" help:"input file to embed into the prompt"`

	PrintPrompt bool `arg:"-p,--prompt" help:"print the rendered prompt for copy-paste"`

	OutputFile       string `arg:"positional" help:"output file. Use - for stdout"`
	ReplaceInputFile bool   `arg:"-r,--replace" help:"inplace rewrite of the input file"`
	NoInput          bool   `arg:"-n,--no-input" help:"use the prompt directly with no input"`
}

type Runner struct {
	args Args
	chat *Chat
}

func (r *Runner) RenderPrompt() (string, *TemplateFrontMatter, error) {
	var err error

	// read prompt file
	prompt, err := os.ReadFile(r.args.PromptFile)
	if err != nil {
		return "", nil, err
	}

	var input []byte

	if !r.args.NoInput {
		if r.args.InputFile == "" {
			// read from stdin as input
			input, err = io.ReadAll(os.Stdin)
			if err != nil {
				return "", nil, err
			}
		} else {
			input, err = os.ReadFile(r.args.InputFile)
			if err != nil {
				return "", nil, err
			}
		}
	}

	return RenderTemplate(string(prompt), TemplateData{
		Input: string(input),
	})
}

// OutputStream produces the output stream of rendered prompt
func (r *Runner) OutputStream(renderedPrompt string, frontMatter *TemplateFrontMatter) (io.ReadCloser, error) {

	stream, err := r.chat.Stream(renderedPrompt, frontMatter)
	if err != nil {
		return nil, err
	}

	return stream, nil
}

// backupFile backups by making a copy suffixed with timestamp
func backupFile(filename string) error {
	// Open the original file for reading
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create the backup filename with the timestamp
	backupFilename := fmt.Sprintf("%s.%s", filename, time.Now().Format(time.RFC3339))

	// Create the backup file for writing
	backupFile, err := os.Create(backupFilename)
	if err != nil {
		return err
	}
	defer backupFile.Close()

	// Copy the contents of the original file to the backup file
	_, err = io.Copy(backupFile, file)
	if err != nil {
		return err
	}

	return nil
}

// ReplaceFile replaces the output file with the output stream, makeing a backupt of the output file first.
func (r *Runner) ReplaceFile(stream io.Reader, outputfile string) error {
	// read output file
	err := backupFile(outputfile)
	if err != nil {
		return err
	}

	// open output file
	f, err := os.OpenFile(outputfile, os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// tee the output to stdout
	stream = io.TeeReader(stream, os.Stdout)

	_, err = io.Copy(f, stream)

	return err
}

func (r *Runner) Run() error {
	prompt, frontMatter, err := r.RenderPrompt()
	if err != nil {
		return err
	}

	if r.args.PrintPrompt {
		fmt.Println(prompt)
		err := clipboard.WriteAll(prompt)
		if err != nil {
			return err
		}
		fmt.Println("[copied to clipboard]")
		return nil
	}

	stream, err := r.OutputStream(prompt, frontMatter)
	if err != nil {
		return err
	}
	defer stream.Close()

	outputFile := r.args.OutputFile
	if r.args.ReplaceInputFile && outputFile == "" {
		outputFile = r.args.InputFile
	}

	if outputFile == "" {
		_, err = io.Copy(os.Stdout, stream)
		return err
	}

	return r.ReplaceFile(stream, outputFile)
}

func run() error {
	var args Args
	arg.MustParse(&args)

	c := openai.NewClient(os.Getenv("OPENAI_SECRET"))
	chat := NewChat(c)

	runner := &Runner{
		args: args,
		chat: chat,
	}

	return runner.Run()
}

func main() {
	err := run()
	if err != nil {
		log.Fatalln(err)
	}
}
