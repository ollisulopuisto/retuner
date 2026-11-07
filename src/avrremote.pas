unit avrremote;

// Retuner: a browser remote for the receiver's own network-audio browser.
//
// When the AVR's source is Internet Radio, the list it is showing IS Retuner's
// menu -- the receiver is our client. Its web interface exposes that list and
// the cursor keys over plain HTTP, so a browser page can drive Retuner's tree
// with a real keyboard instead of the on-screen character picker.
//
// The page cannot talk to the receiver directly: it is served from Retuner, the
// receiver is a different origin, and the receiver sends no CORS headers. So
// everything goes through here. That makes this unit the security boundary, not
// a convenience wrapper -- see RemoteCommandAllowed.

{$mode ObjFPC}{$H+}

interface

uses
  Classes, SysUtils, StrUtils, fphttpclient, fpjson, common;

const
  AVRREMOTE_STATUS_PATH = '/goform/formNetAudio_StatusXml.xml';
  AVRREMOTE_PUT_PATH = '/NetAudio/index.put.asp';
  AVRREMOTE_CMD_PREFIX = 'PutNetAudioCommand/';
  AVRREMOTE_SEARCH_PREFIX = 'PutNetFuncSearchiRadio/';
  AVRREMOTE_UPDATE_CMD = 'aspMainZone_WebUpdateStatus/';

// szLine and chFlag are always ten entries whatever the receiver has to show.
  AVRREMOTE_LINES = 10;

// chFlag is a bitmask, not a value. A real Denon showing search results reports
// 0 for the heading, 1 for every selectable station, and 9 -- 1 or 8 -- for the
// one the cursor is on. Testing chFlag = 8 therefore highlighted nothing on any
// list whose items are selectable, which is every list worth browsing: the
// remote worked and looked like it did not.
  AVRREMOTE_FLAG_SELECTABLE = 1;
  AVRREMOTE_FLAG_CURSOR = 8;

// A keyword long enough to overflow the AVR's own field is not a search, and
// the receiver's on-screen field stops at 30 characters anyway.
  AVRREMOTE_MAX_QUERY = 30;

// The receiver answers one request at a time, and the browser polls this. A
// status fetch that waits fifteen seconds would queue every later poll behind
// it, so this one is deliberately shorter than the general outbound bound.
  AVRREMOTE_IO_TIMEOUT = 4000;         // ms

var
  RemoteAVRAddress: string = '';

function RemoteCommandAllowed(const ACommand: string): boolean;
function RemoteStatusAsJSON: string;
function RemoteSendCommand(const ACommand: string): string;
function RemoteSelect(AIndex: integer): string;
function RemoteSearch(const AQuery: string): string;
function RemoteErrorAsJSON(const AMessage: string): string;

implementation

// Every command the page may send, in full. The page is authenticated, but an
// authenticated page is still a browser, and a proxy that forwards whatever it
// is handed is a general-purpose way to POST anything to the receiver --
// PutZone_InputFunction switches the whole amplifier's source, and a Z2 source
// command powers on zone 2, which here is a pair of speakers outdoors. Matching
// the whole string rather than a prefix is what stops
// "CurDown&cmd1=PutZone_InputFunction/NET" from riding along.
const
  ALLOWED_COMMANDS: array[0..9] of string = (
    'CurUp', 'CurDown', 'CurLeft', 'CurRight', 'CurEnter',
    'CmdPageUp', 'CmdPageDown', 'CmdStop',
    'PresetCall', 'PresetMemo');

function RemoteCommandAllowed(const ACommand: string): boolean;
var
  LCommand: string;
begin
  Result:=False;
  for LCommand in ALLOWED_COMMANDS do
    if ACommand=LCommand then
      Exit(True);
end;

function RemoteErrorAsJSON(const AMessage: string): string;
var
  LObject: TJSONObject;
begin
  LObject:=TJSONObject.Create;
  try
    LObject.Add('ok',False);
    LObject.Add('error',AMessage);
    Result:=LObject.AsJSON;
  finally
    LObject.Free;
  end;
end;

function RemoteBaseURL: string;
begin
  Result:=RemoteAVRAddress.Trim;
  if Result.IsEmpty then
    Exit;
  if not (Result.StartsWith('http://') or Result.StartsWith('https://')) then
    Result:='http://'+Result;
  while Result.EndsWith('/') do
    Result:=Result.Substring(0,Result.Length-1);
end;

function NewClient: TFPHTTPClient;
begin
  Result:=TFPHTTPClient.Create(nil);
  Result.ConnectTimeout:=HTTP_CLIENT_CONNECT_TIMEOUT;
  Result.IOTimeout:=AVRREMOTE_IO_TIMEOUT;
  Result.AllowRedirect:=False;
end;

// The receiver's XML is not namespaced and never nested beyond one level, so a
// scan is enough and avoids pulling a DOM into a path the browser polls. Both
// bounds are checked: a truncated response leaves the closing tag missing, and
// IndexOf then returns -1.
function SectionOf(const AXML, ATag: string): string;
var
  LStart, LEnd: integer;
begin
  Result:='';
  LStart:=AXML.IndexOf('<'+ATag+'>');
  if LStart<0 then
    Exit;
  LStart:=LStart+ATag.Length+2;
  LEnd:=AXML.IndexOf('</'+ATag+'>',LStart);
  if LEnd<0 then
    Exit;
  Result:=AXML.Substring(LStart,LEnd-LStart);
end;

procedure ValuesOf(const ASection: string; AValues: TStrings);
var
  LPos, LStart, LEnd: integer;
begin
  LPos:=0;
  while AValues.Count<AVRREMOTE_LINES do
    begin
      LStart:=ASection.IndexOf('<value>',LPos);
      if LStart<0 then
        Break;
      LStart:=LStart+7;
      LEnd:=ASection.IndexOf('</value>',LStart);
      if LEnd<0 then
        Break;
      AValues.Add(ASection.Substring(LStart,LEnd-LStart));
      LPos:=LEnd+8;
    end;
end;

// The receiver escapes for XML, and these values go into JSON and then into a
// page. Ampersand last: undoing it first would turn "&amp;lt;" into "<".
function XMLDecode(const AText: string): string;
begin
  Result:=AText;
  Result:=Result.Replace('&lt;','<',[rfReplaceAll]);
  Result:=Result.Replace('&gt;','>',[rfReplaceAll]);
  Result:=Result.Replace('&quot;','"',[rfReplaceAll]);
  Result:=Result.Replace('&apos;','''',[rfReplaceAll]);
  Result:=Result.Replace('&amp;','&',[rfReplaceAll]);
end;

// One of the ten lines is a page counter like "   [    1/7  ]" rather than
// something you can select. A remote that treats every non-empty line as a list
// item offers it as a station, which does nothing when you press it and looks
// like the remote is broken. It is recognised here and reported separately.
function LooksLikePageCounter(const AText: string; out APage: string): boolean;
var
  LOpen, LClose: integer;
  LInner: string;
begin
  Result:=False;
  APage:='';
  LOpen:=AText.IndexOf('[');
  LClose:=AText.IndexOf(']');
  if (LOpen<0) or (LClose<LOpen) then
    Exit;
  LInner:=AText.Substring(LOpen+1,LClose-LOpen-1).Trim;
  if not LInner.Contains('/') then
    Exit;
  Result:=True;
  APage:=LInner;
end;

function RemoteStatusAsJSON: string;
var
  LClient: TFPHTTPClient;
  LXML, LPage, LText: string;
  LLines, LFlags: TStringList;
  LResult, LEntry: TJSONObject;
  LItems: TJSONArray;
  i, LFlag: integer;
begin
  if RemoteBaseURL.IsEmpty then
    Exit(RemoteErrorAsJSON('No receiver address configured. Set RemoteAVRAddress in viritin.ini.'));

  LXML:='';
  LClient:=NewClient;
  try
    try
      LXML:=LClient.Get(RemoteBaseURL+AVRREMOTE_STATUS_PATH);
    except
      on E: Exception do
        Exit(RemoteErrorAsJSON('Cannot reach the receiver at '+RemoteAVRAddress+' ('+E.Message+').'));
    end;
  finally
    LClient.Free;
  end;

  LLines:=TStringList.Create;
  LFlags:=TStringList.Create;
  LResult:=TJSONObject.Create;
  try
    ValuesOf(SectionOf(LXML,'szLine'),LLines);
    ValuesOf(SectionOf(LXML,'chFlag'),LFlags);
    if LLines.Count=0 then
      Exit(RemoteErrorAsJSON('The receiver did not return a list.'));

    LItems:=TJSONArray.Create;
    LPage:='';
    for i:=0 to LLines.Count-1 do
      begin
        LText:=XMLDecode(LLines[i]);
        if LText.Trim.IsEmpty then                  // padding up to ten entries
          Continue;
        if LooksLikePageCounter(LText,LPage) then
          Continue;
        LFlag:=0;
        if i<LFlags.Count then
          LFlag:=StrToIntDef(LFlags[i].Trim,0);
        LEntry:=TJSONObject.Create;
        LEntry.Add('index',i);
        LEntry.Add('text',LText);
        LEntry.Add('flag',LFlag);
        // The page asks "is this the cursor", not "what is bit three of a
        // number the receiver's firmware defines". Deciding it here keeps that
        // knowledge in one place, next to the note above that says what the
        // bits mean.
        LEntry.Add('cursor',(LFlag and AVRREMOTE_FLAG_CURSOR)<>0);
        LEntry.Add('selectable',(LFlag and AVRREMOTE_FLAG_SELECTABLE)<>0);
        LItems.Add(LEntry);
      end;
    LResult.Add('ok',True);
    LResult.Add('lines',LItems);
    LResult.Add('page',LPage);
    Result:=LResult.AsJSON;
  finally
    LResult.Free;
    LFlags.Free;
    LLines.Free;
  end;
end;

// Percent-encoding for one form value, leaving the unreserved set alone.
//
// common.pas's URLEncode escapes digits too, which is legal but means a station
// name arrives as %31%30%31; and TFPHTTPClient.FormPost cannot be used at all
// here, because it encodes the value it is handed -- a term encoded on the way
// in came out as rock%2520%2526%2520roll and the receiver searched for that
// literally. The body is therefore built once, here, and posted raw.
function FormEncode(const AValue: string): string;
var
  LChar: AnsiChar;
begin
  Result:='';
  for LChar in AValue do
    if LChar in ['A'..'Z','a'..'z','0'..'9','-','_','.','~'] then
      Result:=Result+LChar
    else
      Result:=Result+'%'+IntToHex(Ord(LChar),2);
end;

// Returns '' on success, a message otherwise.
function PutCommand(const ACmd0: string): string;
var
  LClient: TFPHTTPClient;
  LResponse: TStringStream;
begin
  Result:='';
  if RemoteBaseURL.IsEmpty then
    Exit('No receiver address configured. Set RemoteAVRAddress in viritin.ini.');

  LClient:=NewClient;
  LResponse:=TStringStream.Create('');
  try
    LClient.RequestBody:=TRawByteStringStream.Create(
      'cmd0='+FormEncode(ACmd0)+'&cmd1='+FormEncode(AVRREMOTE_UPDATE_CMD));
    LClient.AddHeader('Content-Type','application/x-www-form-urlencoded');
    try
      try
        LClient.Post(RemoteBaseURL+AVRREMOTE_PUT_PATH,LResponse);
      except
        on E: Exception do
          Result:='Cannot reach the receiver at '+RemoteAVRAddress+' ('+E.Message+').';
      end;
    finally
      LClient.RequestBody.Free;
      LClient.RequestBody:=nil;
    end;
  finally
    LResponse.Free;
    LClient.Free;
  end;
end;

function RemoteSendCommand(const ACommand: string): string;
begin
  if not RemoteCommandAllowed(ACommand) then
    Exit('Not a permitted command.');
  Result:=PutCommand(AVRREMOTE_CMD_PREFIX+ACommand);
end;

// Where the cursor is, or -1. The receiver is the only thing that knows, so
// this asks it rather than tracking a position of our own that would be wrong
// the moment anyone touched the physical remote.
function CursorLine(out ACount: integer): integer;
var
  LClient: TFPHTTPClient;
  LXML: string;
  LFlags: TStringList;
  i: integer;
begin
  Result:=-1;
  ACount:=0;
  LXML:='';
  LClient:=NewClient;
  try
    try
      LXML:=LClient.Get(RemoteBaseURL+AVRREMOTE_STATUS_PATH);
    except
      on E: Exception do
        Exit;
    end;
  finally
    LClient.Free;
  end;

  LFlags:=TStringList.Create;
  try
    ValuesOf(SectionOf(LXML,'chFlag'),LFlags);
    ACount:=LFlags.Count;
    for i:=0 to LFlags.Count-1 do
      if (StrToIntDef(LFlags[i].Trim,0) and AVRREMOTE_FLAG_CURSOR)<>0 then
        Exit(i);
  finally
    LFlags.Free;
  end;
end;

// Clicking a name is cursor keys, counted for you: the distance from where the
// cursor is to where you clicked, then Enter.
//
// It steps here rather than in the page because the receiver answers one
// request at a time -- nine round trips from a browser would be slower, and a
// tab closed halfway through would leave the cursor somewhere nobody chose.
// Nothing new reaches the receiver: every step is CurUp, CurDown or CurEnter,
// each already on the list above, so this is a convenience over the same
// commands and not a wider door.
function RemoteSelect(AIndex: integer): string;
var
  LCursor, LCount, LStep, i: integer;
  LCommand, LError: string;
begin
  if RemoteBaseURL.IsEmpty then
    Exit('No receiver address configured. Set RemoteAVRAddress in viritin.ini.');
  if (AIndex<0) or (AIndex>=AVRREMOTE_LINES) then
    Exit('There is no line '+IntToStr(AIndex)+'.');

  LCursor:=CursorLine(LCount);
  if LCount=0 then
    Exit('The receiver did not return a list.');
  // Guessing would move the selection somewhere nobody asked for, on a screen
  // where Enter might start playing it. A screen with no cursor -- Now Playing
  // reports every flag as 0 -- is one to leave alone.
  if LCursor<0 then
    Exit('The receiver is not showing a list with a cursor.');
  if AIndex>=LCount then
    Exit('There is no line '+IntToStr(AIndex)+'.');

  if AIndex<LCursor then
    LCommand:='CurUp'
  else
    LCommand:='CurDown';

  LStep:=Abs(AIndex-LCursor);
  for i:=1 to LStep do
    begin
      LError:=RemoteSendCommand(LCommand);
      if not LError.IsEmpty then
        Exit(LError);
    end;
  Result:=RemoteSendCommand('CurEnter');
end;

function RemoteSearch(const AQuery: string): string;
var
  LQuery: string;
begin
  LQuery:=AQuery.Trim;
  if LQuery.IsEmpty then
    Exit('Nothing to search for.');
  if LQuery.Length>AVRREMOTE_MAX_QUERY then
    Exit('Search term is too long.');
// Passed through unencoded: PutCommand encodes the whole cmd0 value exactly
// once. Encoding it here too is what produced rock%2520%2526%2520roll.
  Result:=PutCommand(AVRREMOTE_SEARCH_PREFIX+LQuery);
end;

end.
