package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"github.com/astaxie/beego"
)

const beaconURL = "http://www.google-analytics.com/collect"

var (
	pixel        = mustReadFile("static/pixel.gif")
	badge        = mustReadFile("static/badge.svg")
	badgeGif     = mustReadFile("static/badge.gif")
	badgeFlat    = mustReadFile("static/badge-flat.svg")
	badgeFlatGif = mustReadFile("static/badge-flat.gif")
	pageTemplate = template.Must(template.New("page").ParseFiles("ga-beacon/page.html"))
)

func mustReadFile(path string) []byte {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return b
}

func generateUUID(cid *string) error {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return err
	}

	b[8] = (b[8] | 0x80) & 0xBF // what's the purpose ?
	b[6] = (b[6] | 0x40) & 0x4F // what's the purpose ?
	*cid = hex.EncodeToString(b)
	return nil
}

func main() {
	beego.Router("/:ua/*", &AnalyticsController{})
	beego.Run()
	//http.HandleFunc("/", handler)
}

type AnalyticsController struct {
	beego.Controller
}

func (this *AnalyticsController) Get() {
	// this.Ctx.Output.Body([]byte(this.Ctx.Input.Param(":ua") + this.Ctx.Input.Param(":splat")))

	r := this.Ctx.Request

	params := strings.SplitN(strings.Trim(r.URL.Path, "/"), "/", 2)
	query, _ := url.ParseQuery(r.URL.RawQuery)
	refOrg := r.Header.Get("Referer")

	// / -> redirect
	if len(params[0]) == 0 {
		this.Redirect("https://github.com/andelf/ga-beacon", 302)
		return
	}

	// activate referrer path if ?useReferer is used and if referer exists
	if _, ok := query["useReferer"]; ok {
		if len(refOrg) != 0 {
			referer := strings.Replace(strings.Replace(refOrg, "http://", "", 1), "https://", "", 1)
			if len(referer) != 0 {
				// if the useReferer is present and the referer information exists
				//  the path is ignored and the beacon referer information is used instead.
				params = strings.SplitN(strings.Trim(r.URL.Path, "/")+"/"+referer, "/", 2)
			}
		}
	}
	// /account -> account template
	if len(params) == 1 {
		this.TplNames = "page.html"
		this.Data["Account"] = params[0]
		this.Data["Referer"] = refOrg
		return
	}

	// /account/page -> GIF + log pageview to GA collector
	var cid string
	if cookie, err := r.Cookie("cid"); err != nil {
		if err := generateUUID(&cid); err != nil {
			beego.Debug("Failed to generate client UUID:", err)
		} else {
			beego.Debug("Generated new client UUID:", cid)
			this.Ctx.SetCookie("cid", cid, -1, fmt.Sprint("/", params[0]))
		}
	} else {
		cid = cookie.Value
		beego.Debug("Existing CID found: %v", cid)
	}

	if len(cid) != 0 {
		this.Ctx.Output.Header("Cache-Control", "no-cache")
		this.Ctx.Output.Header("CID", cid)

		logHit(params, query, r.Header.Get("User-Agent"), r.RemoteAddr, cid)
	}

	// Write out GIF pixel or badge, based on presence of "pixel" param.
	if _, ok := query["pixel"]; ok {
		this.Ctx.Output.Header("Content-Type", "image/gif")
		this.Ctx.Output.Body(pixel)
	} else if _, ok := query["gif"]; ok {
		this.Ctx.Output.Header("Content-Type", "image/gif")
		this.Ctx.Output.Body(badgeGif)
	} else if _, ok := query["flat"]; ok {
		this.Ctx.Output.Header("Content-Type", "image/svg+xml")
		this.Ctx.Output.Body(badgeFlat)
	} else if _, ok := query["flat-gif"]; ok {
		this.Ctx.Output.Header("Content-Type", "image/gif")
		this.Ctx.Output.Body(badgeFlatGif)
	} else {
		this.Ctx.Output.Header("Content-Type", "image/svg+xml")
		this.Ctx.Output.Body(badge)
	}
}

func log(ua string, ip string, cid string, values url.Values) error {
	req, _ := http.NewRequest("POST", beaconURL, strings.NewReader(values.Encode()))
	req.Header.Add("User-Agent", ua)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}

	if resp, err := client.Do(req); err != nil {
		beego.Error("GA collector POST error:", err.Error())
		return err
	} else {
		beego.Debug("GA collector status:", resp.Status, ", cid:", cid, ", ip:", ip)
		beego.Debug("Reported payload: ", values)
	}
	return nil
}

func logHit(params []string, query url.Values, ua string, ip string, cid string) error {
	// 1) Initialize default values from path structure
	// 2) Allow query param override to report arbitrary values to GA
	//
	// GA Protocol reference: https://developers.google.com/analytics/devguides/collection/protocol/v1/reference

	payload := url.Values{
		"v":   {"1"},        // protocol version = 1
		"t":   {"pageview"}, // hit type
		"tid": {params[0]},  // tracking / property ID
		"cid": {cid},        // unique client ID (server generated UUID)
		"dp":  {params[1]},  // page path
		"uip": {ip},         // IP address of the user
	}

	for key, val := range query {
		payload[key] = val
	}

	return log(ua, ip, cid, payload)
}
