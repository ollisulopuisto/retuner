unit presets;

// Retuner: curated per-country station presets, pulled from a central list.
//
// The problem this solves is the first five minutes. Radio-browser carries
// something like 58,000 stations and holds no opinion about any of them, and a
// receiver's jog dial is a poor instrument for finding the four you actually
// want. A preset file is a short, hand-checked list for one country -- the
// national broadcaster's own streams first -- so a new install has something
// worth listening to before anyone opens a config file.
//
// The files live in presets/ in the Retuner repository and are fetched over
// HTTPS. They are ordinary stations.ini files, so nothing in here has to
// understand their contents: they are handed to my_stations, which already
// knows how to parse, serve and refresh that format. A fetch that fails leaves
// the last good copy in place, so a network outage costs you yesterday's list
// rather than the whole menu.
//
// Only a broadcaster's own published streams belong in these files. Warner
// Music & Sony Music v TuneIn [2021] EWCA Civ 441 turned on who is making the
// communication to the public; a geo-scoped list of official streams keeps this
// a convenience that points at what a broadcaster already publishes, rather
// than a service re-transmitting someone else's catalogue.

{$mode ObjFPC}{$H+}

interface

uses
  Classes, SysUtils, StrUtils, IniFiles,
  common;

const
  PRESETS_DIR = 'presets';
  PRESETS_DEFAULT_URL = 'https://raw.githubusercontent.com/ollisulopuisto/viritin/master/presets';
// A country list is a few dozen lines of text. Anything approaching this cap is
// not one, and the cap is what stops a redirect to something enormous from
// being read into memory before we find that out.
  PRESETS_MAX_BYTES = 512*1024;
  PRESETS_COUNTRY_CODE_LENGTH = 2;

function PresetsCacheDir: string;
function PresetsCountryCodes: TStringArray;
function PresetsCachedFiles: TStringArray;
function RefreshPresets: boolean;

var
  PresetsEnabled: boolean = False;
  PresetsCountries: string = '';
  PresetsURL: string = PRESETS_DEFAULT_URL;
// Minutes. 0 means fetch once at startup and leave it alone; these lists change
// a few times a year, so polling them hourly would be all cost and no news.
  PresetsAutoRefreshPeriod: integer = 0;

implementation

function PresetsCacheDir: string;
begin
  Result:=ConfigPath+DirectorySeparator+PRESETS_DIR;
end;

function PresetsCountryCodes: TStringArray;
var
  LParts: TStringArray;
  LCode: string;
  i,j,k: integer;
  LSeen: boolean;
begin
  Result:=nil;
  LParts:=PresetsCountries.Split([',',';',' '],TStringSplitOptions.ExcludeEmpty);
  SetLength(Result,Length(LParts));
  k:=0;
  for i:=0 to Length(LParts)-1 do
    begin
      LCode:=LowerCase(LParts[i].Trim);
// The code becomes both a path segment and a URL segment. Anything that is not
// exactly two ASCII letters is refused rather than sanitised: a value like
// '../../etc/passwd' should not be repaired into something that still reaches
// somewhere it was never meant to.
      if (Length(LCode)=PRESETS_COUNTRY_CODE_LENGTH) and
         (LCode[1] in ['a'..'z']) and (LCode[2] in ['a'..'z']) then
        begin
          LSeen:=False;
          for j:=0 to k-1 do
            if Result[j]=LCode then
              begin
                LSeen:=True;
                Break;
              end;
          if not LSeen then
            begin
              Result[k]:=LCode;
              k:=k+1;
            end;
        end
      else
        Logging(ltWarning, string.Join(' ',['Presets:','ignoring',QuotedStr(LParts[i].Trim),
                                            '- a country must be a two-letter code such as fi or se']));
    end;
  SetLength(Result,k);
end;

// A CDN answers a missing file with a 200 and a page of HTML more often than it
// ought to, and writing that into the cache would replace a good list with
// markup. A file earns its place only by containing at least one entry whose
// value is an http(s) URL, which is the least a station list can be.
function LooksLikeStationList(const AFileName: string): boolean;
var
  LSections, LEntries: TStrings;
  i,j: integer;
  LValue: string;
begin
  Result:=False;
  LSections:=TStringList.Create;
  LEntries:=TStringList.Create;
  try
    try
      with TIniFile.Create(AFileName) do
        try
          ReadSections(LSections);
          for i:=0 to LSections.Count-1 do
            begin
              LEntries.Clear;
              ReadSectionRaw(LSections[i],LEntries);
              for j:=0 to LEntries.Count-1 do
                begin
                  LValue:=LEntries[j].Substring(LEntries[j].IndexOf('=')+1).Trim;
                  if LValue.StartsWith('http://') or LValue.StartsWith('https://') then
                    begin
                      Result:=True;
                      Exit;
                    end;
                end;
            end;
        finally
          Free;
        end;
    except
      Result:=False;
    end;
  finally
    LSections.Free;
    LEntries.Free;
  end;
end;

function FetchCountry(const ACode: string): boolean;
var
  LStream: TMemoryStream;
  LClient: TLocalHttpClient;
  LURL, LTarget, LTemp: string;
begin
  Result:=False;
  LURL:=PresetsURL.TrimRight(['/'])+'/'+ACode+'.ini';
  LTarget:=PresetsCacheDir+DirectorySeparator+ACode+'.ini';
  LTemp:=LTarget+'.tmp';
  LStream:=TMemoryStream.Create;
  LClient:=TLocalHttpClient.Create(PRESETS_MAX_BYTES);
  try
    LClient.AllowRedirect:=True;
    LClient.AddHeader(HTTP_HEADER_USER_AGENT,VIRITIN_USER_AGENT+'/'+APP_VERSION);
    try
      LClient.Get(LURL,LStream);
    except
      on E: Exception do
        begin
          Logging(ltError, string.Join(' ',['Presets:',MSG_GETTING,MSG_ERROR,LURL,'('+E.Message+')']));
          Exit;
        end;
    end;
    if LStream.Size=0 then
      begin
        Logging(ltWarning, string.Join(' ',['Presets:',LURL,'returned nothing']));
        Exit;
      end;
    try
      LStream.Position:=0;
      LStream.SaveToFile(LTemp);
    except
      on E: Exception do
        begin
          Logging(ltError, string.Join(' ',['Presets:','cannot write',LTemp,'('+E.Message+')']));
          Exit;
        end;
    end;
// Validated in the temporary file and only then moved into place, so a bad
// download never becomes the copy that gets loaded. The previous file stays
// readable throughout.
    if LooksLikeStationList(LTemp) then
      begin
        DeleteFile(LTarget);
        Result:=RenameFile(LTemp,LTarget);
        if Result then
          Logging(ltInfo, string.Join(' ',['Presets:','updated',ACode,'from',LURL]))
        else
          Logging(ltError, string.Join(' ',['Presets:','cannot move',LTemp,'into place']));
      end
    else
      begin
        DeleteFile(LTemp);
        Logging(ltWarning, string.Join(' ',['Presets:',LURL,'is not a station list;',
                                            IfThen(FileExists(LTarget),'keeping the cached copy',
                                                                       'nothing cached for '+ACode)]));
      end;
  finally
    LClient.Free;
    LStream.Free;
  end;
end;

function PresetsCachedFiles: TStringArray;
var
  LCodes: TStringArray;
  i,k: integer;
  LFile: string;
begin
  Result:=nil;
  if not PresetsEnabled then
    Exit;
  LCodes:=PresetsCountryCodes;
  SetLength(Result,Length(LCodes));
  k:=0;
  for i:=0 to Length(LCodes)-1 do
    begin
      LFile:=PresetsCacheDir+DirectorySeparator+LCodes[i]+'.ini';
      if FileExists(LFile) then
        begin
          Result[k]:=LFile;
          k:=k+1;
        end;
    end;
  SetLength(Result,k);
end;

function RefreshPresets: boolean;
var
  LCodes: TStringArray;
  i: integer;
  LFresh: integer = 0;
begin
  Result:=False;
  if not PresetsEnabled then
    Exit;
  LCodes:=PresetsCountryCodes;
  if Length(LCodes)=0 then
    begin
      Logging(ltWarning, 'Presets: enabled, but no usable country code is configured');
      Exit;
    end;
  if not ForceDirectories(PresetsCacheDir) then
    begin
      Logging(ltError, string.Join(' ',['Presets:','cannot create',PresetsCacheDir]));
      Exit;
    end;
  for i:=0 to Length(LCodes)-1 do
    if FetchCountry(LCodes[i]) then
      LFresh:=LFresh+1;
// The distinction worth logging is between "we are running on what we fetched"
// and "we are running on what we had", because the second one is the state a
// user needs to know about when a station list looks out of date.
  Result:=Length(PresetsCachedFiles)>0;
  if LFresh=Length(LCodes) then
    Logging(ltInfo, string.Join(' ',['Presets:',LFresh.ToString,'of',Length(LCodes).ToString,'up to date']))
  else if Result then
    Logging(ltWarning, string.Join(' ',['Presets:','only',LFresh.ToString,'of',Length(LCodes).ToString,
                                        'could be fetched; using the cached copies for the rest']))
  else
    Logging(ltError, 'Presets: nothing could be fetched and nothing is cached');
end;

end.
