unit dnsserver;

// Retuner: DNS proxy serwer unit.

{$mode ObjFPC}{$H+}

interface

uses
  Classes, SysUtils, syncobjs, DateUtils,
  IdUDPServer, IdDNSServer, IdDNSCommon, IdGlobal, IdSocketHandle,
  common;

type
  TIdDNSServerProxy = class(TObject)
    IdDNSServer: TIdDNSServer;
    procedure IdDNS_UDPServerDoAfterQuery(ABinding: TIdSocketHandle; ADNSHeader: TDNSHeader; var QueryResult: TIdBytes; var ResultCode: string; Query : TIdBytes);
  end;

const
  DNS_SERVICE = 'DNS Service';
  DNSSERVER_PORT = 53;
// Every name here belongs to a station directory that is dead or has dropped
// its users, and each is a vTuner-family endpoint this server can answer for.
// wifiradiofrontier.com is Frontier Silicon's: those radios used vTuner as their
// directory until Frontier dropped it in May 2019, and they speak the same
// protocol on a deeper path -- see the Frontier checks in script/smoke-test.sh.
// Deliberately NOT here: frontier-nuvola.net. Nuvola itself shut down on
// 2024-10-31, but that was a handover to airable rather than a switch-off, and
// radios from brands that signed with airable go on reaching a working service
// under those names -- so intercepting them by default would break devices that
// work today. Owners whose brand was dropped in the handover are the ones who
// may want the name; they add it by hand. See doc/OTHER-PORTALS.md.
// This must stay in step with InterceptDNs in cfg/viritin.ini: the Home
// Assistant add-on does not write that key, so this constant is what its users
// actually get.
// radiodenon.com and radioharmankardon.com were added on DNS evidence rather
// than a device report. *.vtuner.com carries a wildcard record, so a brand
// resolving there proves nothing; what does mean something is a name with its
// own A record pointing where a known vTuner host points. radiodenon.com
// answers on the same address as radiomarantz.com and denon.vtuner.com, and
// radioharmankardon.com on the same address as revox.vtuner.com. Both brands
// are already in the confirmed list via *.vtuner.com, so this only adds the
// separate portal name their firmware may ask for instead. See doc/DEVICES.md.
  INTERCEPT_DNS = '*.vtuner.com,*.radiosetup.com,*.my-noxon.net,'+
                  '*.radiomarantz.com,*.radiodenon.com,'+
                  '*.radioharmankardon.com,*.wifiradiofrontier.com';
  DNS_SERVERS = '8.8.8.8,9.9.9.9';
// How long a client stays allowed to have queries forwarded after it was last
// seen on the web service.
  DNS_CLIENT_TTL_HOURS = 24;
// A name is at most 255 bytes on the wire, but the walk that parses one is not
// bounded by that, so the log line is.
  DNS_LOGGED_NAME_MAX = 300;

var
  IdDNSServerProxy: TIdDNSServerProxy;
  DNSServerIPAddress: string;
  DNSServerPort: integer = DNSSERVER_PORT;
  DNSServerEnabled: boolean = True;
  DNSServers: string = DNS_SERVERS;
  InterceptDNs: string = INTERCEPT_DNS;
// Address to put in intercepted answers. Empty means "the address the query
// arrived on", which is right on a LAN but wrong behind NAT, where that is the
// private address and the AVR cannot reach it.
  DNSAdvertiseIP: string = '';
// Off by default so a LAN install keeps forwarding for every device that uses
// it as a resolver. Turn it on when the service is reachable from the internet:
// intercepted names are still answered for anyone, but anything else is
// refused unless the client is known, so it is not an open resolver.
  DNSRestrictForwarding: boolean = False;
  DNSAllowedClients: string = '';
  DNSAnswerBytes: TBytes = ($C0,$0C,          // Name (Bin=1100000000001100 Dec=12). Pointer to first occurrence of the name.
                            $00,$01,          // Type: A (Host Address)
                            $00,$01,          // Class: IN
                            $00,$00,$0E,$10,  // Time to live = 3600s
                            $00,$04,          // Data length = 4
                            $00,$00,$00,$00); // 4 bytes reserved for IPv4 Address

function StartDNSServer:boolean;
// Records a client that has reached the web service, so that the AVR which just
// asked us to resolve vtuner.com can also have its station lookups forwarded
// without anyone configuring an allow list by hand.
procedure DNSNoteWebClient(const AAddress: string);

implementation

var
  DNSClientLock: TCriticalSection;
  DNSKnownClients: TStringList;

procedure DNSNoteWebClient(const AAddress: string);
var
  LIdx: integer;
begin
  if AAddress.Trim.IsEmpty then
    Exit;
  DNSClientLock.Enter;
  try
    LIdx:=DNSKnownClients.IndexOf(AAddress);
    if LIdx<0 then
      DNSKnownClients.AddObject(AAddress,TObject(PtrInt(DateTimeToUnix(Now))))
    else
      DNSKnownClients.Objects[LIdx]:=TObject(PtrInt(DateTimeToUnix(Now)));
  finally
    DNSClientLock.Leave;
  end;
end;

// A query name is whatever a stranger put in a datagram, and it reaches three
// log lines below. Labels may hold any byte, so writing one out raw turns the
// log into a binary file, and gives anyone who can send a packet control
// characters, terminal escapes and convincing-looking forged lines in a journal
// somebody will later read. Printable ASCII survives; everything else is shown
// as its hex value. Found by script/fuzz-test.sh, whose grep stopped counting
// when the log stopped being text.
function SafeName(const AName: string): string;
var
  LCh: char;
begin
  Result:='';
  for LCh in AName do
    if (LCh>=#32) and (LCh<=#126) then
      Result:=Result+LCh
    else
      Result:=Result+'\x'+IntToHex(Ord(LCh),2);
  if Length(Result)>DNS_LOGGED_NAME_MAX then
    Result:=Copy(Result,1,DNS_LOGGED_NAME_MAX)+'...';
end;

function DNSClientAllowed(const AAddress: string): boolean;
var
  LIdx: integer;
  LEntry: string;
begin
  Result:=False;
  for LEntry in DNSAllowedClients.Split([',']) do
    if LEntry.Trim=AAddress then
      Exit(True);
  DNSClientLock.Enter;
  try
    LIdx:=DNSKnownClients.IndexOf(AAddress);
    Result:=(LIdx>=0)
        and (DateTimeToUnix(Now)-PtrInt(DNSKnownClients.Objects[LIdx]) < DNS_CLIENT_TTL_HOURS*3600);
  finally
    DNSClientLock.Leave;
  end;
end;

function StartDNSServer:boolean;
begin
  Result:=True;
  IdDNSServerProxy:=TIdDNSServerProxy.Create;
  IdDNSServerProxy.IdDNSServer:=TIdDNSServer.Create(nil);
  try
    with IdDNSServerProxy.IdDNSServer do
      begin
        TCPACLActive:=False;
        ServerType:=stPrimary;
      end;
    with IdDNSServerProxy.IdDNSServer.UDPTunnel do
      begin
        BufferSize:=8192;
        DefaultPort:=DNSServerPort;
        ThreadedEvent:=True;
        RootDNS_NET.Clear;
        RootDNS_NET.CommaText:=DNSServers;
        OnAfterQuery:=@IdDNSServerProxy.IdDNS_UDPServerDoAfterQuery;
      end;
    with IdDNSServerProxy.IdDNSServer.UDPTunnel.Bindings.Add do
      begin
        IP:=DNSServerIPAddress;
        IPVersion:=Id_IPv4;
      end;
// Only the UDP tunnel is activated. Activating the whole server would also
// bring up Indy's TCP tunnel, which keeps its own default of port 53 whatever
// DNSServerPort says -- so the service quietly took TCP 53 as well, needing
// privileges it was not asked for and colliding with any other resolver on the
// host. Interception is UDP-only, so the TCP side is not wanted at all.
    IdDNSServerProxy.IdDNSServer.UDPTunnel.Active:=True;
  except
    On E: Exception do
      begin
        Result:=False;
        Logging(ltError, 'DNS Server error: '+E.Message);
        if Assigned(IdDNSServerProxy.IdDNSServer) then
          IdDNSServerProxy.IdDNSServer.Free;
        if Assigned(IdDNSServerProxy) then
          IdDNSServerProxy.Free;
      end;
  end;
end;

// Does a query match one of the InterceptDNs patterns?
//
// Two things this gets right that the inline test it replaces did not:
//
// '*.example.com' covers example.com itself, not only names under it. Without
// that, a device asking for the bare name slips through to the real resolver
// and interception silently does nothing -- which is exactly the state
// '*.radiomarantz.com' was in. It is also what dnsmasq's address=/example.com/
// does, and what every router UI a user is likely to reach for means by it.
//
// And the exact-match case is case-insensitive, as DNS is. Resolvers that use
// 0x20 encoding randomise the case of the name they forward, so a
// case-sensitive comparison here fails intermittently and for no visible
// reason. The wildcard branch already ignored case; this makes the two agree.
function InterceptMatches(const AQuery, APattern: string): boolean;
var
  LSuffix: string;
begin
  if APattern.StartsWith('*') then
    begin
      LSuffix:=APattern.Remove(0,1);
      Result:=AQuery.EndsWith(LSuffix,True) or SameText(AQuery,LSuffix.TrimLeft(['.']));
    end
  else
    Result:=SameText(AQuery,APattern);
end;

procedure TIdDNSServerProxy.IdDNS_UDPServerDoAfterQuery(ABinding: TIdSocketHandle; ADNSHeader: TDNSHeader; var QueryResult: TIdBytes; var ResultCode: string; Query : TIdBytes);
var
  LInterceptDN: string;
  LDNQuery: string;
  LAdvertise: string;
  LQueryResult: TBytes;
  LIntercepted: boolean = False;

  function ReplaceSpecSymbol(S: String): String;
  var
    Count : Integer;
  begin
    Count:=0;
    Result:='';
    while True do
      begin
        Count:=Ord(S.Chars[0]);
        Result:=Result+S.Substring(1,Count)+'.';
        S:=S.Remove(0,Count+1);
        if Ord(S.Chars[0])=0 Then
          Break;
      end;
    Result:=Result.TrimRight(['.']);
  end;

begin
  LDNQuery:=ReplaceSpecSymbol(BytesToString(Query,12));
  for LInterceptDN in InterceptDNs.Split([',']) do
    begin
      if InterceptMatches(LDNQuery,LInterceptDN.Trim) then
        begin
          if (ToHex(Query,4,2)='01000001')                           // Standard query & Questions: 1
            and (ToHex(Query,4,Length(Query)-4)='00010001') then     // Type "A" & Class "IN"
            begin
              Logging(ltDebug, 'DNS Query intercept : '+SafeName(LDNQuery));
              AppendBytes(LQueryResult,Query);
              LQueryResult[2]:=$81;            //Flags: 0x8180 Standard query response, No error
              LQueryResult[3]:=$80;            //Flags: 0x8180 Standard query response, No error
              LQueryResult[4]:=$00;            //Questions: 1
              LQueryResult[5]:=$01;            //Questions: 1
              LQueryResult[6]:=$00;            //Answer RRs: 1
              LQueryResult[7]:=$01;            //Answer RRs: 1

              AppendBytes(LQueryResult,DNSAnswerBytes);
// Behind NAT the address the query arrived on is the private one, which is no
// use to the AVR, so a public deployment sets DNSAdvertiseIP explicitly.
              LAdvertise:=DNSAdvertiseIP.Trim;
              if LAdvertise.IsEmpty then
                LAdvertise:=ABinding.IP;
              LQueryResult[Length(LQueryResult)-4]:=StrToInt(LAdvertise.Split(['.'])[0]);
              LQueryResult[Length(LQueryResult)-3]:=StrToInt(LAdvertise.Split(['.'])[1]);
              LQueryResult[Length(LQueryResult)-2]:=StrToInt(LAdvertise.Split(['.'])[2]);
              LQueryResult[Length(LQueryResult)-1]:=StrToInt(LAdvertise.Split(['.'])[3]);
              ResultCode:='RC';
              QueryResult:=LQueryResult;
              LIntercepted:=True;
            end
          else
            begin
              ResultCode:='NA';
              QueryResult:=Query;
              LIntercepted:=True;
            end;
          Break;
        end;
    end;

// The list above is short because it was assembled from the devices people
// happened to own. A name arriving here is either ordinary traffic or a
// receiver asking for a directory nobody has reported yet, and there is no way
// to tell them apart from here - so log it and let the person reading decide.
// This is what doc/DEVICES.md asks a tester to read back.
  if not LIntercepted then
    Logging(ltDebug, 'DNS query not intercepted: '+SafeName(LDNQuery));

// Anything we did not intercept was resolved upstream on the caller's behalf.
// On a public host that would make this an open resolver, so unknown clients
// get a refusal instead of the answer -- small, and useless for amplification.
  if DNSRestrictForwarding and (not LIntercepted) and (not DNSClientAllowed(ABinding.PeerIP)) then
    begin
      Logging(ltDebug, 'DNS forwarding refused for '+ABinding.PeerIP+' ('+SafeName(LDNQuery)+')');
      SetLength(LQueryResult,0);
      AppendBytes(LQueryResult,Query);
      if Length(LQueryResult)>=6 then
        begin
          LQueryResult[2]:=$81;       // response, recursion desired
          LQueryResult[3]:=$85;       // recursion available + RCODE 5 (refused)
          LQueryResult[6]:=$00;       // no answer records
          LQueryResult[7]:=$00;
        end;
      QueryResult:=LQueryResult;
      ResultCode:='RC';
    end;
end;

initialization
  DNSClientLock:=TCriticalSection.Create;
  DNSKnownClients:=TStringList.Create;
  DNSKnownClients.Sorted:=True;

finalization
  DNSKnownClients.Free;
  DNSClientLock.Free;

end.

