//go:build ignore

// Build this synthetic smoke client locally for linux/amd64, mount it read-only
// into the release container, and execute there. It publishes no host port.
package main

import (
 "bytes"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "net/http/cookiejar"
 "os"
 "strings"
 "time"
)

func main(){if err:=run();err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)};fmt.Println("Linux smoke passed")}
func run()error{
 mode:="check";if len(os.Args)>1{mode=os.Args[1]}
 if os.Geteuid()!=65532{return fmt.Errorf("unexpected runtime uid %d",os.Geteuid())}
 if err:=os.WriteFile("/shiliu-smoke-root-write",[]byte("synthetic"),0600);err==nil{return fmt.Errorf("root filesystem is writable")}
 if data,err:=os.ReadFile("/etc/ssl/certs/ca-certificates.crt");err!=nil||len(data)==0{return fmt.Errorf("CA bundle missing")}
 jar,_:=cookiejar.New(nil);client:=&http.Client{Timeout:5*time.Second,Jar:jar};base:="http://127.0.0.1:8080";csrf:=""
 request:=func(method,path string,payload any,want int)([]byte,error){
  var body io.Reader;if payload!=nil{b,e:=json.Marshal(payload);if e!=nil{return nil,e};body=bytes.NewReader(b)}
  req,e:=http.NewRequest(method,base+path,body);if e!=nil{return nil,e};req.Header.Set("Origin",base);req.Header.Set("Content-Type","application/json");if csrf!=""{req.Header.Set("X-CSRF-Token",csrf)}
  resp,e:=client.Do(req);if e!=nil{return nil,e};defer resp.Body.Close();data,e:=io.ReadAll(io.LimitReader(resp.Body,4<<20));if e!=nil{return nil,e};if resp.StatusCode!=want{return nil,fmt.Errorf("%s %s returned %d, want %d",method,path,resp.StatusCode,want)};return data,nil
 }
 for _,path:=range []string{"/healthz","/readyz"}{if _,e:=request("GET",path,nil,200);e!=nil{return e}}
 for _,path:=range []string{"/","/archive","/articles/synthetic-deep-link"}{data,e:=request("GET",path,nil,200);if e!=nil{return e};if !bytes.Contains(bytes.ToLower(data),[]byte("<!doctype html")){return fmt.Errorf("static deep link did not serve HTML: %s",path)}}
 if _,e:=request("GET","/_app/immutable/missing-synthetic.js",nil,404);e!=nil{return e}
 password,e:=os.ReadFile("/smoke/password");if e!=nil{return e}
 data,e:=request("POST","/api/v1/session",map[string]string{"password":strings.TrimSpace(string(password))},200);if e!=nil{return e};var session struct{Authenticated bool `json:"authenticated"`;CSRF string `json:"csrf_token"`};if e=json.Unmarshal(data,&session);e!=nil{return e};if !session.Authenticated||session.CSRF==""{return fmt.Errorf("login session incomplete")};csrf=session.CSRF
 if _,e=request("GET","/api/v1/synthetic-missing",nil,404);e!=nil{return e}
 if mode=="seed"{data,e=request("POST","/api/v1/feeds",map[string]any{"url":"https://example.invalid/linux-smoke-feed","title":"Linux 合成验证","paused":true},201);if e!=nil{return e};var feed struct{ID string `json:"id"`};if e=json.Unmarshal(data,&feed);e!=nil{return e};if feed.ID==""{return fmt.Errorf("feed ID missing")};if e=os.WriteFile("/data/smoke-feed-id",[]byte(feed.ID),0600);e!=nil{return e}}
 id,e:=os.ReadFile("/data/smoke-feed-id");if e!=nil{return e};data,e=request("GET","/api/v1/feeds",nil,200);if e!=nil{return e};var feeds []struct{ID string `json:"id"`;Paused bool `json:"paused"`;Title string `json:"title"`};if e=json.Unmarshal(data,&feeds);e!=nil{return e};if len(feeds)!=1||feeds[0].ID!=string(id)||!feeds[0].Paused||feeds[0].Title!="Linux 合成验证"{return fmt.Errorf("persisted feed/account state differs")};return nil
}
