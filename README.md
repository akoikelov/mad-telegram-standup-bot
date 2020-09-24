# Standup Bot for Telegram
[![Developed by Mad Devs](https://maddevs.io/badge-dark.svg)](https://maddevs.io/)
[![Project Status: Active – The project has reached a stable, usable state and is being actively developed.](https://www.repostatus.org/badges/latest/active.svg)](https://www.repostatus.org/#active)
[![Go Report Card](https://goreportcard.com/badge/github.com/maddevsio/mad-telegram-standup-bot)](https://goreportcard.com/report/github.com/maddevsio/mad-telegram-standup-bot)
[![CircleCI](https://circleci.com/gh/maddevsio/mad-telegram-standup-bot.svg?style=svg)](https://circleci.com/gh/maddevsio/mad-telegram-standup-bot)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Automates workflow of async standups meetings

Supported languages: English(default), Russian 

Add @SimpleStandupBot to your telegram channel to start using it right away.
IMPORTANT NOTE: First you should run the code, then add bot to a channel, only then it would work as expected

## Bot Skills

- Onbords new members with predefined, customized message
- Warns about upcomming standup deadline in case intern did not write standup yet
- Members join and leave standup teams on their own (no time from managers needed)
- Can adjust to different timezones 
- Detects standups by watching messages with bot tag and defined keywords
- supports English and Runssian languages. To add more, see https://github.com/nicksnyder/go-i18n for language reference

## Available commands
```
help - display help text 
join - join standup team
show - displays group info
leave - leave standup team 
edit_deadline - edit group standup deadline (formats: 10am, 13:45)
update_onbording_message - set or update your group greeting message
update_group_language - set or update your group primary language (format: en, ru)
group_tz - update group time zone (default: Asia/Bishkek)
tz - update individual time zone (default: Asia/Bishkek)
change_submission_days - changes days people should submit standups at (format: "monday, tuesday, etc)
```

## Local usage
First you need to set env variables:
```
export TELEGRAM_TOKEN=yourTelegramTokenRecievedFromBotFather
export DEBUG=true
```
Install [dep](https://github.com/golang/dep)

Then run. Note, you need `Docker` and `docker-compose` installed on your system

```
make run
```
To run tests: 
```
make clear
make test
```
To debug locally without docker use:
```
make clear
make setup
go run main.go
```
This should setup a database and run all the migrations for you. 

To update messages: 
First install CLI from [original repo](https://github.com/nicksnyder/go-i18n) then follow these steps:

1. Make changes to your default messages
2. Run `goi18n extract` to update English translation files
3. Create translate.ru.toml file for Russian translations
3. Run `goi18n merge active.*.toml translate.*.toml` to change translated messages to update Russian translations
4. Run `goi18n merge active.*.toml` to update russian translations

## Install on your server 

1. Build and push bot's image to Dockerhub or any other container registry: 
```
docker build  -t <youraccount>/mad-telegram-standup-bot  .
```
```
docker push <youraccount>/mad-telegram-standup-bot
```
2. Enter server, install `docker` and `docker-compose` there. Create `docker-compose.yaml` file by the example from this repo
3. Create `.env` file with variables needed to run bot:
```
TELEGRAM_TOKEN=603860531:AAEB95f4tq18RWZtKLFJDFLKFDFfdsfds
DEBUG=false
```
4. Pull image from registry and run it in the backgroud with
```
docker-compose pull
```
```
docker-compose up -d
```

## Contribution

Any contribution is welcomed! Fork it and send PR or simply request a feature or point to bug with issues. 
