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
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
)

const warnPeriod = 10 // 10 minutes before the deadline

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
				Other: "Attention, {{.Standuper}} {{.Warn}} minutes till deadline, submit standups ASAP.",
			},
			TemplateData: map[string]interface{}{
				"Standuper": key,
				"Warn":      warnPeriod,
			},
		})
		if err != nil {
			log.Error(err)
		}
		text += warn
	}

	msg := tgbotapi.NewMessage(group.ChatID, text)
	_, err = b.tgAPI.Send(msg)
	if err != nil {
		log.WithFields(logrus.Fields{
			"err":   err,
			"msg":   msg,
			"func":  "WarnGroup",
			"group": group,
		}).Error("tgAPI.Send failed")
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
		log.WithFields(logrus.Fields{
			"group": group,
			"err":   err,
		}).Error("ListChatStandupers failed")
		return
	}

	if len(standupers) == 0 {
		return
	}

	missed := map[string]int{}

	t = t.Add(time.Duration(b.c.NotificationTime) * time.Minute)

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

		_, err := b.db.CreateNotificationThread(model.NotificationThread{
			ChatID:           standuper.ChatID,
			Username:         standuper.Username,
			NotificationTime: t,
			ReminderCounter:  0,
		})
		if err != nil {
			log.Error("Error on executing CreateNotificationThread ", err, "ChatID: ", standuper.ChatID, "Username: ", standuper.Username)
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
				Other: "Attention, {{.Standuper}}! you have just missed the deadline! submit standups ASAP!",
			},
			TemplateData: map[string]interface{}{
				"Standuper": key,
			},
		})
		if err != nil {
			log.Error(err)
		}
		text += notify
	}

	msg := tgbotapi.NewMessage(group.ChatID, text)
	_, err = b.tgAPI.Send(msg)
	if err != nil {
		log.WithFields(logrus.Fields{
			"err":   err,
			"msg":   msg,
			"func":  "NotifyGroup",
			"group": group,
		}).Error("tgAPI.Send failed")
	}
}

//CheckNotificationThread notify users
func (b *Bot) CheckNotificationThread(group *model.Group, t time.Time) {
	localizer := i18n.NewLocalizer(b.bundle, group.Language)
	if strings.TrimSpace(group.StandupDeadline) == "" {
		threads, err := b.db.ListNotificationsThread(group.ChatID)
		if err != nil {
			log.Error("Error on executing ListNotificationThread! ", err, "group.ChatID: ", group.ChatID, "GroupTitle: ", group.Title)
			return
		}
		for _, thread := range threads {
			err = b.db.DeleteNotificationThread(thread.ID)
			if err != nil {
				log.Error("Error on executing DeleteNotificationsThread! ", err, "Thread ID: ", thread.ID)
				continue
			}
		}
	}

	threads, err := b.db.ListNotificationsThread(group.ChatID)
	if err != nil {
		log.Error("Error on executing ListNotificationThread! ", err, "group.ChatID: ", group.ChatID, "GroupTitle: ", group.Title)
		return
	}

	for _, thread := range threads {
		stillSubmitStandups := false

		standupers, err := b.db.ListChatStandupers(thread.ChatID)
		if err != nil {
			log.Error("ListChatStandupers ", err, "Chat ID: ", thread.ChatID)
		}

		for _, standuper := range standupers {
			if standuper.Username == thread.Username {
				stillSubmitStandups = true
			}
		}

		if !stillSubmitStandups {
			err = b.db.DeleteNotificationThread(thread.ID)
			if err != nil {
				log.Error("Error on executing DeleteNotificationsThread! ", err, "Thread ID: ", thread.ID)
			}
			continue
		}

		loc, err := time.LoadLocation(group.TZ)

		if t.Hour() != thread.NotificationTime.In(loc).Hour() || t.Minute() != thread.NotificationTime.In(loc).Minute() {
			continue
		}

		if thread.ReminderCounter >= b.c.MaxReminders {
			err = b.db.DeleteNotificationThread(thread.ID)
			if err != nil {
				log.Error("Error on executing DeleteNotificationsThread! ", err, "Thread ID: ", thread.ID)
			}
			continue
		}

		if b.submittedStandupToday(model.Standuper{
			Username: thread.Username,
			ChatID:   thread.ChatID,
			TZ:       group.TZ,
		}) {
			err = b.db.DeleteNotificationThread(thread.ID)
			if err != nil {
				log.Error("Error on executing DeleteNotificationsThread! ", err, "Thread ID: ", thread.ID)
			}
			continue
		}

		thread.NotificationTime = thread.NotificationTime.Add(time.Duration(b.c.NotificationTime) * time.Minute)
		err = b.db.UpdateNotificationThread(thread.ID, thread.ChatID, thread.NotificationTime)

		if err != nil {
			log.Error("Error on executing UpdateNotificationThread! ", err, "Thread ID: ", thread.ID)
			continue
		}

		notify, err := localizer.Localize(&i18n.LocalizeConfig{
			DefaultMessage: &i18n.Message{
				ID:    "remindNonReporter",
				Other: "Attention, @{{.Standuper}}! you still haven't written a standup! Write a standup!",
			},
			TemplateData: map[string]interface{}{
				"Standuper": thread.Username,
			},
		})

		if err != nil {
			log.Error("Error on localize, CheckNotificationThread! ", err)
		}

		msg := tgbotapi.NewMessage(group.ChatID, notify)
		_, err = b.tgAPI.Send(msg)
		if err != nil {
			log.WithFields(logrus.Fields{
				"err":    err,
				"msg":    msg,
				"group":  group,
				"notify": notify,
			}).Error("tgAPI.Send failed")
			if strings.Contains(err.Error(), "bot was kicked from the group chat") {

				err := b.db.DeleteGroupStandupers(group.ChatID)
				if err != nil {
					return
				}
				err = b.db.DeleteGroup(group.ChatID)
				if err != nil {
					return
				}
				return
			}
		}
	}
}
