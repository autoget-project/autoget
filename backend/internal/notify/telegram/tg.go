package telegram

import (
	"context"

	"github.com/autoget-project/autoget/backend/internal/notify"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type Config struct {
	Token  string `yaml:"token"`
	ChatID string `yaml:"chat_id"`
}

var _ notify.INotifier = (*Notifier)(nil)

type Notifier struct {
	config *Config
	bot    *bot.Bot
}

func New(config *Config) (*Notifier, error) {
	b, err := bot.New(config.Token)
	if err != nil {
		return nil, err
	}

	return &Notifier{
		config: config,
		bot:    b,
	}, nil
}

func (n *Notifier) SendMessage(message string) error {
	_, err := n.bot.SendMessage(context.Background(), &bot.SendMessageParams{
		ChatID: n.config.ChatID,
		Text:   message,
	})

	return err
}

func (n *Notifier) SendMarkdownMessage(message string) error {
	_, err := n.bot.SendMessage(context.Background(), &bot.SendMessageParams{
		ChatID:    n.config.ChatID,
		Text:      message,
		ParseMode: models.ParseModeMarkdown,
	})

	return err
}
