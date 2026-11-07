unit relayserver;

// Retuner: HTTP relay for stations these AVRs cannot fetch themselves.
//
// A growing share of stations are HTTPS-only and vTuner-era firmware has no
// TLS, so the existing "all-as-http" setting -- which only rewrites the scheme
// and hopes the station still answers on port 80 -- increasingly fails. With
// the relay enabled Retuner fetches such a stream itself and re-serves it over
// plain HTTP.
//
// This needs its own listener rather than a route on the web server: fcl-web
// frames a response from ContentLength and reads the content stream exactly
// once, so it cannot emit an open-ended stream. Here the socket is ours, so
// bytes are forwarded as they arrive and the connection lasts as long as the
// listener keeps playing.

{$mode ObjFPC}{$H+}

interface

uses
  Classes, SysUtils, StrUtils, ssockets,
{$IFDEF UNIX}
  BaseUnix, Sockets,
{$ENDIF}
  fphttpclient, common;

const
  RELAY_PORT = 8888;
  RELAY_PATH = '/relay';
  RELAY_SERVICE = 'Relay Service';
  RELAY_REQUEST_MAX = 4096;
  RELAY_DEFAULT_CONTENT_TYPE = 'audio/mpeg';
// A bridge that restarts, or a station that drops, should not cost the listener
// their station. Reconnect while the AVR is still there, giving up only after
// this many attempts deliver nothing at all.
  RELAY_RECONNECT_ATTEMPTS = 5;
  RELAY_RECONNECT_PAUSE_MS = 2000;
// Before a single byte has arrived, the source is not one that dropped -- it is
// one that was never there. A bridge that is still starting up deserves one
// more try; a wrong or dead URL should reach the device as an error quickly
// rather than after ten seconds of silence.
  RELAY_INITIAL_ATTEMPTS = 2;

type
// Resolves a station id to the URL to pull from. Supplied by httpserver so this
// unit does not have to depend on it.
  TRelayResolveURL = function(const AID: string): string;

// Everything TFPHTTPClient writes here goes straight out to the AVR, so an
// endless stream is forwarded chunk by chunk and never accumulates.
//
// Our own response headers are not sent until the first upstream byte arrives.
// By then the client has parsed the station's headers, so we can pass on its
// real content type and its ICY metadata interval rather than guessing. The
// object outlives a reconnect, so headers are sent once per listener.
  TRelayForwardStream = class(TStream)
  private
    FTarget: TStream;
    FClient: TFPHTTPClient;
    FHeadersSent: boolean;
    FWantIcy: boolean;
    FIcyActive: boolean;
    FDownstreamFailed: boolean;
    FBytes: Int64;
    procedure SendDownstreamHeaders;
  public
    constructor Create(ATarget: TStream; AWantIcy: boolean);
    function Write(const Buffer; Count: Longint): Longint; override;
    function Read(var Buffer; Count: Longint): Longint; override;
    function Seek(const Offset: Int64; Origin: TSeekOrigin): Int64; override;
    property Client: TFPHTTPClient read FClient write FClient;
    property HeadersSent: boolean read FHeadersSent;
    property DownstreamFailed: boolean read FDownstreamFailed;
    property IcyActive: boolean read FIcyActive;
    property Bytes: Int64 read FBytes;
  end;

var
  RelayHTTPS: boolean = False;
  RelayPort: integer = RELAY_PORT;
// Set to the web server's address so the relay is reachable exactly where the
// menus are, and nowhere else. Empty binds every interface.
  RelayIPAddress: string = '';
  OnRelayResolveURL: TRelayResolveURL = nil;

function StartRelayServer: boolean;
procedure StopRelayServer;
function RelayURLFor(const AStationID: string): string;
function NeedsRelay(const AURL: string): boolean;

implementation

type
// TInetServer calls OnConnect on its accept thread, so handling a stream there
// would serve one listener at a time and, worse, block new connections for as
// long as a relay runs. Each stream gets its own thread instead.
  TRelayConnection = class(TThread)
  private
    FSocket: TSocketStream;
  protected
    procedure Execute; override;
  public
    constructor Create(ASocket: TSocketStream);
  end;

  TRelayListener = class(TThread)
  private
    FServer: TInetServer;
    procedure HandleConnect(Sender: TObject; Data: TSocketStream);
  protected
    procedure Execute; override;
  public
    destructor Destroy; override;
    procedure Stop;
  end;

var
  RelayListener: TRelayListener = nil;

constructor TRelayForwardStream.Create(ATarget: TStream; AWantIcy: boolean);
begin
  inherited Create;
  FTarget:=ATarget;
  FWantIcy:=AWantIcy;
  FHeadersSent:=False;
  FIcyActive:=False;
  FDownstreamFailed:=False;
  FBytes:=0;
end;

procedure TRelayForwardStream.SendDownstreamHeaders;
var
  LHead, LContentType, LMetaInt: string;

  function Upstream(const AName: string): string;
  begin
    Result:='';
    if Assigned(FClient) then
      Result:=FClient.ResponseHeaders.Values[AName].Trim;
  end;

  procedure Pass(const AName: string);
  begin
    if Upstream(AName)<>'' then
      LHead:=LHead+AName+': '+Upstream(AName)+#13#10;
  end;

begin
  LContentType:=Upstream('Content-Type');
  if LContentType.IsEmpty then
    LContentType:=RELAY_DEFAULT_CONTENT_TYPE;
  LHead:='HTTP/1.0 200 OK'+#13#10
        +'Server: '+VIRITIN_USER_AGENT+'/'+APP_VERSION+#13#10
        +'Content-Type: '+LContentType+#13#10;
// Station name, genre and bitrate cost nothing to relay and some devices show
// them, so pass on whatever the station supplied.
  Pass('icy-name');
  Pass('icy-genre');
  Pass('icy-br');
// Interleaved titles are only forwarded when the AVR asked for them. Announcing
// a metadata interval to a device that did not would make it play the metadata
// as if it were audio.
  LMetaInt:=Upstream('icy-metaint');
  if FWantIcy and (LMetaInt<>'') then
    begin
      LHead:=LHead+'icy-metaint: '+LMetaInt+#13#10;
      FIcyActive:=True;
    end;
  LHead:=LHead+'Connection: close'+#13#10#13#10;
  FTarget.WriteBuffer(LHead[1],Length(LHead));
end;

function TRelayForwardStream.Write(const Buffer; Count: Longint): Longint;
begin
  if not FHeadersSent then
    begin
      SendDownstreamHeaders;
      FHeadersSent:=True;
    end;
// A write that fails means the AVR hung up -- changed station or powered off.
// Letting it raise unwinds the fetch, which is exactly what should happen, but
// it is recorded first so the caller can tell this from the upstream dropping
// and not spend its reconnect attempts on a listener that has gone.
  try
    FTarget.WriteBuffer(Buffer,Count);
  except
    FDownstreamFailed:=True;
    raise;
  end;
  Inc(FBytes,Count);
  Result:=Count;
end;

function TRelayForwardStream.Read(var Buffer; Count: Longint): Longint;
begin
  Result:=0;
end;

function TRelayForwardStream.Seek(const Offset: Int64; Origin: TSeekOrigin): Int64;
begin
  Result:=0;
end;

function NeedsRelay(const AURL: string): boolean;
begin
  Result:=RelayHTTPS and AURL.ToLower.StartsWith('https://');
end;

function RelayURLFor(const AStationID: string): string;
begin
  Result:='http://'+URLHost+':'+RelayPort.ToString+RELAY_PATH+'?'+PATH_PARAM_ID+'='+AStationID;
end;

// Pulls the station id out of the request line, e.g. GET /relay?id=RB_X HTTP/1.1
function RelayRequestedID(const ARequestLine: string): string;
var
  LTarget: string;
begin
  Result:='';
  if Length(ARequestLine.Split([' ']))<2 then
    Exit;
  LTarget:=ARequestLine.Split([' '])[1];
  if not LTarget.StartsWith(RELAY_PATH) then
    Exit;
  if not LTarget.Contains(PATH_PARAM_ID+'=') then
    Exit;
  Result:=LTarget.Substring(LTarget.IndexOf(PATH_PARAM_ID+'=')+Length(PATH_PARAM_ID)+1).Split(['&'])[0];
end;

function ReadLine(ASocket: TStream; var ACount: integer): string;
var
  LByte: Byte = 0;
begin
  Result:='';
  while ACount<RELAY_REQUEST_MAX do
    begin
      if ASocket.Read(LByte,1)<>1 then
        Break;
      Inc(ACount);
      if LByte=10 then
        Break;
      if LByte<>13 then
        Result:=Result+Chr(LByte);
    end;
end;

// Reads the request line and drains the headers, reporting whether the device
// asked for interleaved titles. The headers have to be consumed either way, or
// they would sit in the socket buffer.
function ReadRequest(ASocket: TStream; out AWantIcy: boolean): string;
var
  LCount: integer = 0;
  LLine: string;
begin
  AWantIcy:=False;
  Result:=ReadLine(ASocket,LCount);
  repeat
    LLine:=ReadLine(ASocket,LCount);
    if LLine.ToLower.StartsWith('icy-metadata:') and LLine.Contains('1') then
      AWantIcy:=True;
  until LLine.IsEmpty or (LCount>=RELAY_REQUEST_MAX);
end;

procedure SendRelayStatus(ASocket: TStream; const AStatus, AExtraHeaders: string);
var
  LHead: string;
begin
  LHead:='HTTP/1.0 '+AStatus+#13#10
        +'Server: '+VIRITIN_USER_AGENT+'/'+APP_VERSION+#13#10
        +AExtraHeaders
        +'Connection: close'+#13#10#13#10;
  ASocket.WriteBuffer(LHead[1],Length(LHead));
end;

procedure RelayStation(ASocket: TSocketStream);
var
  LRequestLine, LID, LURL: string;
  LForward: TRelayForwardStream;
  LWantIcy: boolean = False;
  LAttempts: integer = 0;
  LLimit: integer = RELAY_INITIAL_ATTEMPTS;
  LBefore: Int64;
  LClient: TFPHTTPClient;
begin
  LRequestLine:=ReadRequest(ASocket,LWantIcy);
  LID:=RelayRequestedID(LRequestLine);
  if LID.IsEmpty or (not Assigned(OnRelayResolveURL)) then
    begin
      SendRelayStatus(ASocket,'404 Not Found','');
      Exit;
    end;
  LURL:=OnRelayResolveURL(LID);
  if ResolvePlaylists and LooksLikePlaylist(LURL) then
    LURL:=ResolveStreamURL(LURL);
  if LURL.IsEmpty then
    begin
      Logging(ltWarning, string.Join(' ',[RELAY_SERVICE+':',MSG_STATIONS,MSG_NOT_FOUND,LID]));
      SendRelayStatus(ASocket,'404 Not Found','');
      Exit;
    end;

  Logging(ltDebug, string.Join(' ',[RELAY_SERVICE+':',LID,'->',LURL]));
  LForward:=TRelayForwardStream.Create(ASocket,LWantIcy);
  try
    repeat
      LBefore:=LForward.Bytes;
      LClient:=TFPHTTPClient.Create(nil);
      try
        LForward.Client:=LClient;
        LClient.AllowRedirect:=True;
        LClient.ConnectTimeout:=HTTP_CLIENT_CONNECT_TIMEOUT;
// No IOTimeout: a live stream is meant to stay open indefinitely, and the read
// only ends when the station stops or the AVR disconnects.
        LClient.IOTimeout:=0;
        LClient.AddHeader(HTTP_HEADER_USER_AGENT,VIRITIN_USER_AGENT+'/'+APP_VERSION);
        if LWantIcy then
          LClient.AddHeader('Icy-MetaData','1');
        try
          LClient.Get(LURL,LForward);
        except
          on E: Exception do
            Logging(ltDebug, string.Join(' ',[RELAY_SERVICE+':','upstream ended',LID,'('+E.Message+')']));
        end;
      finally
        LForward.Client:=nil;
        LClient.Free;
      end;

// Anything delivered means the source was alive, so a later drop is a fresh
// problem and gets the full allowance of retries again.
      if LForward.Bytes>LBefore then
        LAttempts:=0
      else
        Inc(LAttempts);
      if LForward.Bytes>0 then
        LLimit:=RELAY_RECONNECT_ATTEMPTS
      else
        LLimit:=RELAY_INITIAL_ATTEMPTS;

// Nothing to reconnect for if it is the listener that left.
      if LForward.DownstreamFailed then
        Break;
// Interleaved titles are framed by a byte count running from the start of the
// response. A reconnect restarts that count upstream while the device keeps
// counting from where it was, so every later title would be played as audio.
// Dropping the connection instead lets the AVR re-request the station cleanly.
      if LForward.IcyActive then
        begin
          Logging(ltDebug, string.Join(' ',[RELAY_SERVICE+':','not resuming a metadata stream',LID]));
          Break;
        end;
      if LAttempts>=LLimit then
        begin
          Logging(ltDebug, string.Join(' ',[RELAY_SERVICE+':','giving up on',LID]));
          Break;
        end;
      Sleep(RELAY_RECONNECT_PAUSE_MS);
      Logging(ltDebug, string.Join(' ',[RELAY_SERVICE+':','reconnecting',LID]));
// A write to a departed AVR raises, which leaves this loop by way of the
// handler's exception guard rather than spinning on a dead socket.
    until False;
// Headers are held back until the first upstream byte, so a source that never
// answered has left the device holding a connection with no HTTP response on
// it at all. Say what happened instead of just hanging up.
    if not LForward.HeadersSent then
      try
        Logging(ltWarning, string.Join(' ',[RELAY_SERVICE+':','no audio from',LID,'->',LURL]));
        SendRelayStatus(ASocket,'502 Bad Gateway','');
      except
        on E: Exception do
          Logging(ltDebug, string.Join(' ',[RELAY_SERVICE+':',MSG_ERROR,'('+E.Message+')']));
      end;
  finally
    LForward.Free;
  end;
end;

constructor TRelayConnection.Create(ASocket: TSocketStream);
begin
  inherited Create(True);
  FSocket:=ASocket;
  FreeOnTerminate:=True;
  Start;
end;

procedure TRelayConnection.Execute;
begin
  try
    try
      RelayStation(FSocket);
    except
      on E: Exception do
        Logging(ltDebug, string.Join(' ',[RELAY_SERVICE+':',MSG_ERROR,'('+E.Message+')']));
    end;
  finally
    FSocket.Free;
  end;
end;

procedure TRelayListener.HandleConnect(Sender: TObject; Data: TSocketStream);
begin
  TRelayConnection.Create(Data);
end;

procedure TRelayListener.Execute;
{$IFDEF UNIX}
var
  LReuse: longint;
{$ENDIF}
begin
  try
    if RelayIPAddress.IsEmpty then
      FServer:=TInetServer.Create(RelayPort)
    else
      FServer:=TInetServer.Create(RelayIPAddress,RelayPort);
    FServer.OnConnect:=@HandleConnect;
{$IFDEF UNIX}
// A relayed connection is closed by us, not the listener, so the port is left
// in TIME_WAIT and a restart within a minute or so cannot rebind it -- the
// relay would come up dead while the rest of the service ran normally. Setting
// the option on the socket directly rather than through TInetServer.ReuseAddress
// because that property does not reach the socket in FPC 3.2.2 (verified: with
// a TIME_WAIT entry on the port, the property leaves bind failing and this call
// makes it succeed).
    LReuse:=1;
    fpsetsockopt(FServer.Socket,SOL_SOCKET,SO_REUSEADDR,@LReuse,SizeOf(LReuse));
{$ENDIF}
    FServer.Bind;
    FServer.Listen;
// Logged here rather than by the caller: StartRelayServer only reports that the
// thread began, and the bind can still fail after that.
    Logging(ltInfo, RELAY_SERVICE+': listening on: '+IfThen(RelayIPAddress.IsEmpty,'*',RelayIPAddress)+':'+RelayPort.ToString);
    FServer.StartAccepting;
  except
    on E: Exception do
      if not Terminated then
        Logging(ltError, string.Join(' ',[RELAY_SERVICE+':',MSG_ERROR,'('+E.Message+')']));
  end;
end;

procedure TRelayListener.Stop;
begin
  Terminate;
  if Assigned(FServer) then
    FServer.StopAccepting(True);
end;

destructor TRelayListener.Destroy;
begin
  FreeAndNil(FServer);
  inherited Destroy;
end;

function StartRelayServer: boolean;
begin
  Result:=False;
  try
    RelayListener:=TRelayListener.Create(True);
    RelayListener.FreeOnTerminate:=False;
    RelayListener.Start;
    Result:=True;
  except
    on E: Exception do
      Logging(ltError, string.Join(' ',[RELAY_SERVICE+':',MSG_ERROR,'('+E.Message+')']));
  end;
end;

procedure StopRelayServer;
begin
  if Assigned(RelayListener) then
    begin
      RelayListener.Stop;
      FreeAndNil(RelayListener);
    end;
end;

initialization
{$IFDEF UNIX}
// Writing to a socket the listener has already closed raises SIGPIPE, and its
// default action is to kill the process -- so every time an AVR changed station
// mid-stream it would take Retuner with it. Ignored process-wide, the write
// reports an ordinary error instead, which the relay already handles by
// unwinding the fetch. The web server never hit this because its responses are
// short enough to complete before a client can walk away mid-write.
  FpSignal(SIGPIPE, SignalHandler(SIG_IGN));
{$ENDIF}

end.
