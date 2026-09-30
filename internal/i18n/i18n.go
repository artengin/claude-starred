package i18n

import (
	"fmt"
	"os"
	"strings"
)

var english = map[string]string{
	"title":           "Starred",
	"empty":           "Nothing starred yet. Run /star inside a Claude Code session.",
	"lost":            "(lost)",
	"hint_rename":     "rename",
	"hint_unstar":     "unstar",
	"hint_paths":      "paths",
	"hint_help":       "help",
	"prompt_rename":   "Name: ",
	"confirm_unstar":  "Unstar «%s»? (y/n)",
	"confirm_last":    "Claude already deleted «%s», this is the last copy. Unstar and lose it? (y/n)",
	"confirm_live":    "«%s» is open in another terminal. Open it here as well? (y/n)",
	"renamed":         "Renamed: %s",
	"unstarred":       "Unstarred: %s",
	"lost_session":    "The transcript is gone, the session cannot be opened",
	"sync_failed":     "Could not keep a copy: %s",
	"help":            "Keys",
	"help_move":       "down / up",
	"help_open":       "open",
	"help_back":       "back",
	"help_edges":      "first / last",
	"help_rename":     "rename",
	"help_unstar":     "unstar",
	"help_paths":      "names / full paths",
	"help_quit":       "quit",
	"help_close":      "Any key to go back",
	"moved_from":      "%s is gone, opening from %s",
	"star_done":       "Starred «%s».",
	"no_transcript":   "session %s has no saved messages yet: send any message in it, then run /star again",
	"star_snapshot":   "Hard link failed, so the kept copy is a snapshot; it is refreshed each time you open `starred`.",
	"star_status_no":  "not starred",
	"star_status_yes": "starred",
}

var russian = map[string]string{
	"title":           "Избранное",
	"empty":           "В избранном пусто. Выполните /star внутри сессии Claude Code.",
	"lost":            "(потеряна)",
	"hint_rename":     "имя",
	"hint_unstar":     "убрать",
	"hint_paths":      "пути",
	"hint_help":       "помощь",
	"prompt_rename":   "Имя: ",
	"confirm_unstar":  "Убрать «%s» из избранного? (y/n)",
	"confirm_last":    "Claude уже удалил «%s», это последняя копия. Убрать и потерять её? (y/n)",
	"confirm_live":    "«%s» открыта в другом терминале. Открыть и здесь? (y/n)",
	"renamed":         "Переименовано: %s",
	"unstarred":       "Убрано из избранного: %s",
	"lost_session":    "Транскрипта нет, сессию не открыть",
	"sync_failed":     "Не удалось сохранить копию: %s",
	"help":            "Клавиши",
	"help_move":       "вниз / вверх",
	"help_open":       "открыть",
	"help_back":       "назад",
	"help_edges":      "в начало / в конец",
	"help_rename":     "переименовать",
	"help_unstar":     "убрать из избранного",
	"help_paths":      "имена / полные пути",
	"help_quit":       "выход",
	"help_close":      "Любая клавиша, чтобы вернуться",
	"moved_from":      "Каталога %s больше нет, открываю из %s",
	"star_done":       "«%s» в избранном.",
	"no_transcript":   "в сессии %s ещё нет сохранённых сообщений: отправьте в ней любое сообщение и снова запустите /star",
	"star_snapshot":   "Hard link не создался, копия сохранена снимком и обновляется при каждом открытии `starred`.",
	"star_status_no":  "не в избранном",
	"star_status_yes": "в избранном",
}

func messages() map[string]string {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := os.Getenv(name); value != "" {
			if strings.HasPrefix(value, "ru") {
				return russian
			}

			return english
		}
	}

	return english
}

func T(key string, args ...any) string {
	message, ok := messages()[key]

	if !ok {
		message = english[key]
	}

	if len(args) == 0 {
		return message
	}

	return fmt.Sprintf(message, args...)
}
