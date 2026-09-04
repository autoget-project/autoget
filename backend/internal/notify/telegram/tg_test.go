package telegram

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSendMessage(t *testing.T) {
	token := os.Getenv("TG_TOKEN")
	chatID := os.Getenv("TG_CHAT_ID")

	if token == "" || chatID == "" {
		t.Skip("no env TG_TOKEN or TG_CHAT_ID")
	}

	bot, err := New(&Config{
		Token:  token,
		ChatID: chatID,
	})
	require.NoError(t, err)

	require.NoError(t, bot.SendMessage("test message"))
}

func TestSendMarkdownMessage(t *testing.T) {
	token := os.Getenv("TG_TOKEN")
	chatID := os.Getenv("TG_CHAT_ID")

	if token == "" || chatID == "" {
		t.Skip("no env TG_TOKEN or TG_CHAT_ID")
	}

	bot, err := New(&Config{
		Token:  token,
		ChatID: chatID,
	})
	require.NoError(t, err)

	require.NoError(t, bot.SendMarkdownMessage(`*title*:
  test message`))
}
