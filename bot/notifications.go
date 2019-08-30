package bot

import (
	"strings"
	"time"

	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/maddevsio/mad-telegram-standup-bot/model"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/olebedev/when"
	"github.com/olebedev/when/rules/en"
	"github.com/olebedev/when/rules/ru"
	log "github.com/sirupsen/logrus"
)

const warnPeriod = 10       // 10 minutes before the deadline
const notificationTime = 30 // 30 minutes after deadline
const maxReminder = 3

var reminderCounter int

//StartWatchers looks for new gropus from the channel and start watching it
func (b *Bot) StartWatchers() {
	for group := range b.watchersChan {
		log.Info("New group to track: ", group)
		team := &model.Team{
			Group:    group,
			QuitChan: make(chan struct{}),
		}
		b.teams = append(b.teams, team)
		b.wg.Add(1)
		go b.trackStandupersIn(team)
		b.wg.Done()
	}
}

func (b *Bot) trackStandupersIn(team *model.Team) {
	ticker := time.NewTicker(time.Second * 60).C
	for {
		select {
		case <-ticker:
			loc, err := time.LoadLocation(team.Group.TZ)
			if err != nil {
				log.Error("failed to load location for ", team.Group)
				continue
			}
			b.WarnGroup(team.Group, time.Now().In(loc))
			b.CheckNotificationThread(team.Group, time.Now().In(loc))
			b.NotifyGroup(team.Group, time.Now().In(loc))

		case <-team.QuitChan:
			log.Info("Finish working with the group: ", team.QuitChan)
			return
		}
	}
}

//WarnGroup launches go routines that warns standupers
//about upcoming deadlines
func (b *Bot) WarnGroup(group *model.Group, t time.Time) {
	localizer := i18n.NewLocalizer(b.bundle, group.Language)

	if !shouldSubmitStandupIn(group, t) {
		return
	}

	if group.StandupDeadline == "" {
		return
	}
	w := when.New(nil)
	w.Add(en.All...)
	w.Add(ru.All...)

	r, err := w.Parse(group.StandupDeadline, time.Now())
	if err != nil {
		log.Errorf("Unable to parse channel standup time [%v]: [%v]", group.StandupDeadline, err)
		return
	}

	if r == nil {
		log.Errorf("Could not find matches. Channel standup time: [%v]", group.StandupDeadline)
		return
	}

	t = t.Add(warnPeriod * time.Minute)

	if t.Hour() != r.Time.Hour() || t.Minute() != r.Time.Minute() {
		return
	}

	standupers, err := b.db.ListChatStandupers(group.ChatID)
	if err != nil {
		log.Error(err)
		return
	}

	if len(standupers) == 0 {
		return
	}

	stillDidNotSubmit := map[string]int{}

	for _, standuper := range standupers {
		if b.submittedStandupToday(standuper) {
			continue
		}
		if standuper.Username == "" {
			username := fmt.Sprintf("[stranger](tg://user?id=%v)", standuper.UserID)
			stillDidNotSubmit[username] = standuper.Warnings
		} else {
			stillDidNotSubmit["@"+standuper.Username] = standuper.Warnings
		}

	}

	//? if everything is fine, should not bother the team...
	if len(stillDidNotSubmit) == 0 {
		return
	}

	var text string

	for key := range stillDidNotSubmit {
		warn, err := localizer.Localize(&i18n.LocalizeConfig{
			DefaultMessage: &i18n.Message{
				ID:    "warnNonReporters",
				Other: "Attention, {{.Intern}} {{.Warn}} minutes till deadline, submit standups ASAP.",
			},
			TemplateData: map[string]interface{}{
				"Intern": key,
				"Warn":   warnPeriod,
			},
		})
		if err != nil {
			log.Error(err)
		}
		text += warn
	}

	msg := tgbotapi.NewMessage(group.ChatID, text)
	msg.ParseMode = "Markdown"
	_, err = b.tgAPI.Send(msg)
	if err != nil {
		log.Error(err)
	}
}

//NotifyGroup launches go routines that notify standupers
//about upcoming deadlines
func (b *Bot) NotifyGroup(group *model.Group, t time.Time) {
	localizer := i18n.NewLocalizer(b.bundle, group.Language)

	if !shouldSubmitStandupIn(group, t) {
		return
	}

	if group.StandupDeadline == "" {
		return
	}

	w := when.New(nil)
	w.Add(en.All...)
	w.Add(ru.All...)

	r, err := w.Parse(group.StandupDeadline, time.Now())
	if err != nil {
		log.Errorf("Unable to parse channel standup time [%v]: [%v]", group.StandupDeadline, err)
		return
	}

	if r == nil {
		log.Errorf("Could not find matches. Channel standup time: [%v]", group.StandupDeadline)
		return
	}

	if t.Hour() != r.Time.Hour() || t.Minute() != r.Time.Minute() {
		return
	}

	standupers, err := b.db.ListChatStandupers(group.ChatID)
	if err != nil {
		log.Error(err)
		return
	}

	if len(standupers) == 0 {
		return
	}

	missed := map[string]int{}

	for _, standuper := range standupers {
		if b.submittedStandupToday(standuper) {
			continue
		}

		if standuper.Username == "" {
			username := fmt.Sprintf("[stranger](tg://user?id=%v)", standuper.UserID)
			missed[username] = standuper.Warnings
		} else {
			missed["@"+standuper.Username] = standuper.Warnings
		}

    t = t.Add(notificationTime * time.Minute)

		_, err := b.db.CreateNotificationThread(model.NotificationThread{
			ChatID:           standuper.ChatID,
			UserID:           standuper.UserID,
			NotificationTime: t,
			ReminderCounter:  0,
		})
		if err != nil {
			log.Error(err)
		}
	}

	//? if everything is fine, should not bother the team...
	if len(missed) == 0 {
		return
	}

	var text string

	for key := range missed {
		notify, err := localizer.Localize(&i18n.LocalizeConfig{
			DefaultMessage: &i18n.Message{
				ID:    "notifyNonReporters",
				Other: "Attention, {{.Intern}}! you have just missed the deadline! submit standups ASAP!",
			},
			TemplateData: map[string]interface{}{
				"Intern": key,
			},
		})
		if err != nil {
			log.Error(err)
		}
		text += notify
	}

	msg := tgbotapi.NewMessage(group.ChatID, text)
	msg.ParseMode = "Markdown"
	_, err = b.tgAPI.Send(msg)
	if err != nil {
		log.Error(err)
	}
}

//CheckNotificationThread notify users
func (b *Bot) CheckNotificationThread(group *model.Group, t time.Time) {
	if maxReminder == reminderCounter {
		return
	}

	if strings.TrimSpace(group.StandupDeadline) == "" {
		return
	}
	loc, err := time.LoadLocation(group.TZ)
	if err != nil {
		return
	}

	localizer := i18n.NewLocalizer(b.bundle, group.Language)
	var timeInDB time.Time
	notifications, err := b.db.ListNotificationsThread()
	if err != nil {
		log.Error(err)
	}

	for _, notification := range notifications {
		timeInDB = notification.NotificationTime
	}

	if t.Hour() != timeInDB.In(loc).Hour() || t.Minute() != timeInDB.In(loc).Minute() {
		return
	}

	standupers, err := b.db.ListChatStandupers(group.ChatID)
	if err != nil {
		log.Error(err)
		return
	}

	if len(standupers) == 0 {
		return
	}

	missed := map[string]int{}

	for _, standuper := range standupers {
		if b.submittedStandupToday(standuper) {
			thread, err := b.db.SelectNotificationThread(standuper.UserID, standuper.ChatID)
			if err != nil {
				log.Error(err)
			}
			err = b.db.DeleteNotificationThread(thread.ID)
			if err != nil {
				log.Error(err)
			}
			continue
		}
		timeInDB = timeInDB.Add(notificationTime * time.Minute)

		err = b.db.UpdateNotificationThread(standuper.UserID, timeInDB)
		if err != nil {
			log.Error(err)
		}

		thread, err := b.db.SelectNotificationThread(standuper.UserID, standuper.ChatID)
		if err != nil {
			log.Error(err)
		}

		reminderCounter = thread.ReminderCounter

		if standuper.Username == "" {
			username := fmt.Sprintf("[stranger](tg://user?id=%v)", standuper.UserID)
			missed[username] = standuper.Warnings
		} else {
			missed["@"+standuper.Username] = standuper.Warnings
		}
	}

	if len(missed) == 0 {
		return
	}

	var text string

	for key := range missed {
		notify, err := localizer.Localize(&i18n.LocalizeConfig{
			DefaultMessage: &i18n.Message{
				ID:    "notifyNonReporters",
				Other: "Attention, {{.Intern}}! you have just missed the deadline! submit standups ASAP!",
			},
			TemplateData: map[string]interface{}{
				"Intern": key,
			},
		})
		if err != nil {
			log.Error(err)
		}
		text += notify
	}

	msg := tgbotapi.NewMessage(group.ChatID, text)
	msg.ParseMode = "Markdown"
	_, err = b.tgAPI.Send(msg)
	if err != nil {
		log.Error(err)
	}
}
