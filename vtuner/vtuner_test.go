package vtuner

import (
	"encoding/xml"
	"testing"
)

func TestPageXML(t *testing.T) {
	page := &Page{}
	page.Add(Directory{
		Title: "Test Dir",
		UrlDir: "http://test/dir",
		UrlDirBackUp: "http://test/dir",
		DirCount: 5,
	})

	page.Add(&Station{
		StationId: "123",
		StationName: "Test Station",
	})

	xmlStr, err := page.ToXMLString()
	if err != nil {
		t.Fatalf("Failed to convert to xml: %v", err)
	}

	expected := xml.Header + `<ListOfItems><ItemCount>2</ItemCount><Item><ItemType>Dir</ItemType><Title>Test Dir</Title><UrlDir>http://test/dir</UrlDir><UrlDirBackUp>http://test/dir</UrlDirBackUp><DirCount>5</DirCount></Item><Item><ItemType>Station</ItemType><StationId>123</StationId><StationName>Test Station</StationName><StationUrl></StationUrl><StationDesc></StationDesc><Logo></Logo><StationFormat></StationFormat><StationLocation></StationLocation><StationBandWidth></StationBandWidth><StationMime></StationMime><Relia></Relia><Bookmark></Bookmark></Item></ListOfItems>`
	if xmlStr != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, xmlStr)
	}
}
