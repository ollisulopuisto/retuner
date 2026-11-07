unit podcasts;

// Retuner: Podcasts from RSS feeds.
//
// A podcast maps onto the vTuner model almost exactly: the feed is a directory
// and each episode is a station whose URL is the enclosure. Nothing about the
// receiver has to understand podcasts -- it is browsing folders and playing
// streams, which is all it ever did.
//
// Feeds are listed in podcasts.ini, one per line, in the same shape as the
// stations file:
//
//   [Podcasts]
//   The Documentary=https://podcasts.files.bbci.co.uk/p02nrsl1.rss
//   Radiolab=https://feeds.simplecast.com/EmVW7VGp|https://example.com/art.jpg
//
// Episode lists are fetched on demand and cached, because the receiver pages
// through a directory a screenful at a time and re-fetching a feed for every
// page would make browsing unusable.

{$mode ObjFPC}{$H+}

interface

uses
  Classes, SysUtils, DateUtils, IniFiles, syncobjs,
  laz2_DOM, laz2_XMLRead,
  md5, common;

const
  PODCASTS_FILE_NAME = 'podcasts.ini';
  PODCASTS_PREFIX = 'PC';
  PODCAST_FEED_PREFIX = 'PF';
// A feed is text, and a long-running weekly show can reach a few hundred
// entries. Both caps are about keeping one bad feed from costing memory or a
// jog dial from having to travel through a decade of back catalogue.
  PODCAST_MAX_BYTES = 4*1024*1024;
  PODCAST_EPISODES_LIMIT = 100;
  PODCAST_CACHE_TTL = 60;                 // minutes
  PODCAST_ID_LENGTH = 12;

type
  TPodcastEpisode = record
                      PEID, PETitle, PEURL, PEDescription, PEDate: string;
                    end;

  TPodcastEpisodes = array of TPodcastEpisode;

  TPodcastFeed = record
                   PFID, PFName, PFURL, PFLogoURL, PFGroup: string;
                 end;

  TPodcastFeeds = array of TPodcastFeed;

var
  PodcastsEnabled: boolean = False;
  PodcastsFileName: string = PODCASTS_FILE_NAME;
  PodcastEpisodesLimit: integer = PODCAST_EPISODES_LIMIT;
  PodcastCacheTTL: integer = PODCAST_CACHE_TTL;

function LoadPodcastFeeds: boolean;
function GetPodcastFeeds: TPodcastFeeds;
function GetPodcastFeedByID(const AID: string; out AFeed: TPodcastFeed): boolean;
function GetPodcastEpisodes(const AFeed: TPodcastFeed; out AEpisodes: TPodcastEpisodes): boolean;
function GetPodcastEpisodeByID(const AID: string; out AEpisode: TPodcastEpisode): boolean;
function GetPodcastLogoForEpisode(const AID: string): string;
function PodcastID(const APrefix, AText: string): string;

implementation

type
  TPodcastCacheEntry = record
                         CFeedID: string;
                         CEpisodes: TPodcastEpisodes;
                         CEOL: TDateTime;
                       end;

var
// Feeds are replaced wholesale on reload and read from request threads, so
// readers take a snapshot of the array reference the way the stations list does.
  PodcastLock: TCriticalSection;
  PodcastFeeds: TPodcastFeeds;
  PodcastCache: array of TPodcastCacheEntry;

function PodcastID(const APrefix, AText: string): string;
begin
  Result:=APrefix+'_'+MD5Print(MD5String(AText)).Substring(0,PODCAST_ID_LENGTH).ToUpper;
end;

function GetPodcastFeeds: TPodcastFeeds;
begin
  PodcastLock.Enter;
  try
    Result:=PodcastFeeds;
  finally
    PodcastLock.Leave;
  end;
end;

function GetPodcastFeedByID(const AID: string; out AFeed: TPodcastFeed): boolean;
var
  LFeeds: TPodcastFeeds;
  i: integer;
begin
  Result:=False;
  LFeeds:=GetPodcastFeeds;
  for i:=0 to High(LFeeds) do
    if LFeeds[i].PFID=AID then
      begin
        AFeed:=LFeeds[i];
        Exit(True);
      end;
end;

// Every section is read, and its name travels with the feed only as a label --
// grouping podcasts behind another menu level would put a decade of back
// catalogue three turns of a jog dial away.
function LoadPodcastFeeds: boolean;
var
  LSections, LEntries: TStrings;
  LFileName, LValue: string;
  LFeeds: TPodcastFeeds;
  LCount: integer = 0;
  i, j: integer;
begin
  Result:=False;
  LFileName:=ConfigPath+DirectorySeparator+PodcastsFileName;
  if not FileExists(LFileName) then
    begin
      Logging(ltWarning, string.Join(' ',['Podcasts:',MSG_FILE,MSG_NOT_FOUND,LFileName]));
      Exit;
    end;
  LSections:=TStringList.Create;
  LEntries:=TStringList.Create;
  try
    with TIniFile.Create(LFileName) do
      try
        try
          ReadSections(LSections);
          for i:=0 to LSections.Count-1 do
            begin
              LEntries.Clear;
              ReadSectionValues(LSections[i],LEntries);
              SetLength(LFeeds,LCount+LEntries.Count);
              for j:=0 to LEntries.Count-1 do
                begin
                  if LEntries.Names[j].Trim.IsEmpty then
                    Continue;
                  LValue:=LEntries.ValueFromIndex[j].Trim;
                  if LValue.IsEmpty then
                    Continue;
                  with LFeeds[LCount] do
                    begin
                      PFName:=LEntries.Names[j].Trim;
                      PFGroup:=LSections[i];
                      PFURL:=LValue.Split(['|'])[0].Trim;
                      if Length(LValue.Split(['|']))>1 then
                        PFLogoURL:=LValue.Split(['|'])[1].Trim;
                      PFID:=PodcastID(PODCAST_FEED_PREFIX,PFURL);
                    end;
                  if not LFeeds[LCount].PFURL.IsEmpty then
                    LCount:=LCount+1;
                end;
            end;
// Lines that were blank or malformed left a slot behind, which would show on
// the receiver as an entry with no name.
          SetLength(LFeeds,LCount);
          Result:=LCount>0;
        except
          on E: Exception do
            Logging(ltError, string.Join(' ',['Podcasts:',MSG_ERROR,LFileName,'('+E.Message+')']));
        end;
      finally
        Free;
      end;
    PodcastLock.Enter;
    try
      PodcastFeeds:=LFeeds;
      SetLength(PodcastCache,0);
    finally
      PodcastLock.Leave;
    end;
    Logging(ltInfo, string.Join(' ',['Podcasts:',MSG_SUCCESSFULLY_LOADED+LCount.ToString,'feed(s)']));
  finally
    LEntries.Free;
    LSections.Free;
  end;
end;

function FetchFeed(const AURL: string; AStream: TStream): boolean;
var
  LClient: TLocalHttpClient;
begin
  Result:=False;
  LClient:=TLocalHttpClient.Create(PODCAST_MAX_BYTES);
  try
    LClient.AllowRedirect:=True;
    LClient.AddHeader(HTTP_HEADER_USER_AGENT,VIRITIN_USER_AGENT+'/'+APP_VERSION);
    try
      LClient.Get(AURL,AStream);
      Result:=AStream.Size>0;
    except
      on E: Exception do
        Logging(ltError, string.Join(' ',['Podcasts:',MSG_GETTING,MSG_ERROR,AURL,'('+E.Message+')']));
    end;
  finally
    LClient.Free;
  end;
end;

function NodeText(ANode: TDOMNode; const AName: string): string;
var
  LChild: TDOMNode;
begin
  Result:='';
  if not Assigned(ANode) then
    Exit;
  LChild:=ANode.FindNode(AName);
// A node with no text child is normal in these feeds -- an empty <description>
// serialises exactly that way -- so this must not be assumed to have one.
  if Assigned(LChild) and LChild.HasChildNodes then
    Result:=LChild.FirstChild.NodeValue;
end;

function NodeAttr(ANode: TDOMNode; const AName: string): string;
var
  LAttr: TDOMNode;
begin
  Result:='';
  if not Assigned(ANode) or not Assigned(ANode.Attributes) then
    Exit;
  LAttr:=ANode.Attributes.GetNamedItem(AName);
  if Assigned(LAttr) then
    Result:=LAttr.NodeValue;
end;

// Reduces "Wed, 04 Jun 2025 05:00:00 +0000" to "4 Jun 2025". These displays are
// narrow and the time of day tells the listener nothing.
function ShortDate(const APubDate: string): string;
var
  LParts: TStringArray;
begin
  Result:='';
  LParts:=APubDate.Trim.Replace(',',' ').Split([' '],TStringSplitOptions.ExcludeEmpty);
  if Length(LParts)<4 then
    Exit(APubDate.Trim);
// A leading weekday is optional in RFC 822, so the day number is whichever of
// the first two fields is numeric.
  if StrToIntDef(LParts[0],0)>0 then
    Result:=string.Join(' ',[LParts[0],LParts[1],LParts[2]])
  else
    Result:=string.Join(' ',[LParts[1],LParts[2],LParts[3]]);
end;

function ParseFeed(AStream: TStream; out AEpisodes: TPodcastEpisodes): boolean;
var
  LXML: TXMLDocument = nil;
  LChannel, LItem, LEnclosure: TDOMNode;
  LURL, LType: string;
  LCount: integer = 0;
  i: integer;
begin
  Result:=False;
  SetLength(AEpisodes,0);
  AStream.Position:=0;
  try
    try
      ReadXMLFile(LXML,AStream);
    except
      on E: Exception do
        begin
          Logging(ltError, string.Join(' ',['Podcasts:','feed is not readable XML','('+E.Message+')']));
          Exit;
        end;
    end;
    if not Assigned(LXML) or not Assigned(LXML.DocumentElement) then
      Exit;
// RSS 2.0 only: the root is <rss> with a <channel>. Atom feeds (<feed> with
// <entry>) are not podcast feeds in practice and are left alone rather than
// half-parsed.
    LChannel:=LXML.DocumentElement.FindNode('channel');
    if not Assigned(LChannel) then
      Exit;
    SetLength(AEpisodes,LChannel.ChildNodes.Count);
    for i:=0 to LChannel.ChildNodes.Count-1 do
      begin
        if LCount>=PodcastEpisodesLimit then
          Break;
        LItem:=LChannel.ChildNodes[i];
        if LItem.NodeName<>'item' then
          Continue;
        LEnclosure:=LItem.FindNode('enclosure');
        LURL:=NodeAttr(LEnclosure,'url').Trim;
// An item with no enclosure is not an episode -- feeds carry announcements and
// text-only posts as items too.
        if LURL.IsEmpty then
          Continue;
        LType:=NodeAttr(LEnclosure,'type').ToLower;
        if (not LType.IsEmpty) and (not LType.StartsWith('audio')) then
          Continue;
        with AEpisodes[LCount] do
          begin
            PETitle:=NodeText(LItem,'title').Trim;
            if PETitle.IsEmpty then
              PETitle:='Episode '+(LCount+1).ToString;
            PEURL:=LURL;
            PEDate:=ShortDate(NodeText(LItem,'pubDate'));
            PEDescription:=PEDate;
            PEID:=PodcastID(PODCASTS_PREFIX,LURL);
          end;
        LCount:=LCount+1;
      end;
    SetLength(AEpisodes,LCount);
    Result:=LCount>0;
  finally
    if Assigned(LXML) then
      LXML.Free;
  end;
end;

function CachedEpisodes(const AFeedID: string; out AEpisodes: TPodcastEpisodes): boolean;
var
  i: integer;
begin
  Result:=False;
  PodcastLock.Enter;
  try
    for i:=0 to High(PodcastCache) do
      if (PodcastCache[i].CFeedID=AFeedID) and (PodcastCache[i].CEOL>Now) then
        begin
          AEpisodes:=PodcastCache[i].CEpisodes;
          Exit(True);
        end;
  finally
    PodcastLock.Leave;
  end;
end;

procedure CacheEpisodes(const AFeedID: string; const AEpisodes: TPodcastEpisodes);
var
  i: integer;
begin
  PodcastLock.Enter;
  try
    for i:=0 to High(PodcastCache) do
      if PodcastCache[i].CFeedID=AFeedID then
        begin
          PodcastCache[i].CEpisodes:=AEpisodes;
          PodcastCache[i].CEOL:=IncMinute(Now,PodcastCacheTTL);
          Exit;
        end;
    SetLength(PodcastCache,Length(PodcastCache)+1);
    with PodcastCache[High(PodcastCache)] do
      begin
        CFeedID:=AFeedID;
        CEpisodes:=AEpisodes;
        CEOL:=IncMinute(Now,PodcastCacheTTL);
      end;
  finally
    PodcastLock.Leave;
  end;
end;

function GetPodcastEpisodes(const AFeed: TPodcastFeed; out AEpisodes: TPodcastEpisodes): boolean;
var
  LStream: TMemoryStream;
begin
  SetLength(AEpisodes,0);
  if CachedEpisodes(AFeed.PFID,AEpisodes) then
    Exit(True);
  Result:=False;
  LStream:=TMemoryStream.Create;
  try
    if not FetchFeed(AFeed.PFURL,LStream) then
      Exit;
    if not ParseFeed(LStream,AEpisodes) then
      begin
        Logging(ltWarning, string.Join(' ',['Podcasts:','no episodes in',AFeed.PFName]));
        Exit;
      end;
    CacheEpisodes(AFeed.PFID,AEpisodes);
    Logging(ltDebug, string.Join(' ',['Podcasts:',AFeed.PFName,'->',Length(AEpisodes).ToString,'episode(s)']));
    Result:=True;
  finally
    LStream.Free;
  end;
end;

// Looks in the cache first, since an episode can only have been chosen from a
// listing that filled it. A cache that has expired in between is refilled by
// walking the feeds, which is slower but keeps a stale bookmark playable.
function GetPodcastEpisodeByID(const AID: string; out AEpisode: TPodcastEpisode): boolean;
var
  LFeeds: TPodcastFeeds;
  LEpisodes: TPodcastEpisodes;
  i, j: integer;
begin
  Result:=False;
  PodcastLock.Enter;
  try
    for i:=0 to High(PodcastCache) do
      for j:=0 to High(PodcastCache[i].CEpisodes) do
        if PodcastCache[i].CEpisodes[j].PEID=AID then
          begin
            AEpisode:=PodcastCache[i].CEpisodes[j];
            Exit(True);
          end;
  finally
    PodcastLock.Leave;
  end;

  LFeeds:=GetPodcastFeeds;
  for i:=0 to High(LFeeds) do
    if GetPodcastEpisodes(LFeeds[i],LEpisodes) then
      for j:=0 to High(LEpisodes) do
        if LEpisodes[j].PEID=AID then
          begin
            AEpisode:=LEpisodes[j];
            Exit(True);
          end;
end;

// Episodes carry no artwork of their own here, so the feed's logo stands in for
// all of them -- which is also what a podcast app shows. Only the cache is
// consulted: the episode was listed to be chosen, so its feed is in there, and
// an icon is not worth downloading a feed for.
function GetPodcastLogoForEpisode(const AID: string): string;
var
  LFeedID: string = '';
  LFeed: TPodcastFeed;
  i, j: integer;
begin
  Result:='';
  PodcastLock.Enter;
  try
    for i:=0 to High(PodcastCache) do
      for j:=0 to High(PodcastCache[i].CEpisodes) do
        if PodcastCache[i].CEpisodes[j].PEID=AID then
          begin
            LFeedID:=PodcastCache[i].CFeedID;
            Break;
          end;
  finally
    PodcastLock.Leave;
  end;
  if (not LFeedID.IsEmpty) and GetPodcastFeedByID(LFeedID,LFeed) then
    Result:=LFeed.PFLogoURL;
end;

initialization
  PodcastLock:=TCriticalSection.Create;

finalization
  PodcastLock.Free;

end.
