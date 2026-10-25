package compat

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestPHPSDKInterop executes the original bundled SDK code, with no payment
// endpoint call. Opt in with GOEPAY_PHP_INTEROP=1 and an available Docker daemon.
func TestPHPSDKInterop(t *testing.T) {
	if os.Getenv("GOEPAY_PHP_INTEROP") != "1" {
		t.Skip("set GOEPAY_PHP_INTEROP=1 for Docker PHP original SDK interoperability")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"v1", "v2"} {
		t.Run(version, func(t *testing.T) {
			archiveName := "SDK.zip"
			if version == "v2" {
				archiveName = "SDK_2.0.zip"
			}
			archive, err := zip.OpenReader(filepath.Join("..", "..", "epay_pro", "public", "static", "files", archiveName))
			if err != nil {
				t.Fatal(err)
			}
			defer archive.Close()
			var sdk []byte
			for _, entry := range archive.File {
				if filepath.Base(entry.Name) == "EpayCore.class.php" {
					r, err := entry.Open()
					if err != nil {
						t.Fatal(err)
					}
					sdk, err = io.ReadAll(r)
					r.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			if len(sdk) == 0 {
				t.Fatal("original SDK class not found")
			}
			hash := sha256.Sum256(sdk)
			t.Logf("original %s EpayCore SHA256=%s", archiveName, hex.EncodeToString(hash[:]))
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "sdk.php"), sdk, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "runner.php"), []byte(phpSDKRunner), 0600); err != nil {
				t.Fatal(err)
			}
			cases := []Params{
				{"pid": "1000", "money": "1.00", "out_trade_no": "test", "zero": "0", "empty": ""},
				{"pid": "1000", "money": "1.0", "name": "中文😀 +%", "extension": "unknown", "space": " \t"},
				{"pid": "1000", "money": "0.01", "name": " leading and trailing ", "callback": "https://example.com/?a=1&b=2"},
			}
			for _, p := range cases {
				p["timestamp"] = fmt.Sprint(time.Now().Unix())
				p["sign_type"] = "MD5"
				if version == "v2" {
					p["sign_type"] = "RSA"
				}
			}
			input := map[string]any{"version": version, "private_key": base64.StdEncoding.EncodeToString(privateDER), "public_key": base64.StdEncoding.EncodeToString(publicDER), "cases": cases}
			for _, p := range cases {
				if version == "v1" {
					p["sign"] = SignMD5(p, "secret")
				} else {
					p["sign"], err = SignRSAWithProfile(p, key, ProfileSDKV2)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			payload, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--network", "none", "-i", "--mount", "type=bind,src="+dir+",dst=/work,readonly", "php:8.2-cli", "php", "/work/runner.php")
			cmd.Stdin = bytes.NewReader(payload)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("PHP SDK run: %v %s", err, output)
			}
			var results []struct {
				Signature string `json:"signature"`
				Valid     bool   `json:"valid"`
				Canonical string `json:"canonical"`
			}
			if err := json.Unmarshal(output, &results); err != nil {
				t.Fatalf("%v output=%s", err, output)
			}
			if len(results) != len(cases) {
				t.Fatal("missing SDK results")
			}
			for i, result := range results {
				if !result.Valid || result.Signature != cases[i]["sign"] {
					t.Errorf("SDK interoperability case %d: valid=%v matching-signature=%v", i, result.Valid, result.Signature == cases[i]["sign"])
				}
				if version == "v2" && result.Canonical != CanonicalSDK(cases[i]) {
					t.Errorf("canonical mismatch case %d", i)
				}
			}
		})
	}
}

const phpSDKRunner = `<?php
require '/work/sdk.php';
$in=json_decode(stream_get_contents(STDIN),true,512,JSON_THROW_ON_ERROR);
$config=['apiurl'=>'https://example.invalid/','pid'=>'1000','key'=>'secret','platform_public_key'=>$in['public_key'],'merchant_private_key'=>$in['private_key']];
$sdk=new EpayCore($config);
$method=new ReflectionMethod(EpayCore::class,'getSign');
$method->setAccessible(true);
$out=[];
foreach($in['cases'] as $params){
 $signature=$method->invoke($sdk,$params);
 if($in['version']==='v1'){
  $_GET=$params;
  $valid=$sdk->verifyNotify();
  $canonical='';
 }else{
  $valid=$sdk->verify($params);
  $cm=new ReflectionMethod(EpayCore::class,'getSignContent');
  $cm->setAccessible(true);
  $canonical=$cm->invoke($sdk,$params);
 }
 $out[]=['signature'=>$signature,'valid'=>$valid,'canonical'=>$canonical];
}
echo json_encode($out,JSON_UNESCAPED_UNICODE|JSON_THROW_ON_ERROR);
`
