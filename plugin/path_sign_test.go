//
// Copyright 2021 Outfox, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package jwtsecrets

import (
	"context"
	"fmt"
	"github.com/go-test/deep"
	"github.com/hashicorp/vault/sdk/logical"
	"gopkg.in/square/go-jose.v2/jwt"
	"testing"
)

func getSignedToken(b *backend, storage *logical.Storage, role string, claims map[string]interface{}, dest interface{}) error {
	data := map[string]interface{}{
		"claims": claims,
	}

	req := &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "sign/" + role,
		Storage:   *storage,
		Data:      data,
	}

	resp, err := b.HandleRequest(context.Background(), req)
	if err != nil || (resp != nil && resp.IsError()) {
		return fmt.Errorf("err:%s resp:%#v", err, resp)
	}

	rawToken, ok := resp.Data["token"]
	if !ok {
		return fmt.Errorf("no returned token")
	}

	strToken, ok := rawToken.(string)
	if !ok {
		return fmt.Errorf("token was %T, not a string", rawToken)
	}

	token, err := jwt.ParseSigned(strToken)
	if err != nil {
		return fmt.Errorf("error parsing jwt: %s", err)
	}

	publicKeys, err := FetchJWKS(b, storage)
	if err != nil {
		return fmt.Errorf("error retrieving public keys: %s", err)
	}

	matchingPublicKeys := publicKeys.Key(token.Headers[0].KeyID)
	if len(matchingPublicKeys) != 1 {
		return fmt.Errorf("error locating unique public keys: %s", err)
	}

	var target interface{}
	if dest != nil {
		target = dest
	} else {
		target = &jwt.Claims{}
	}

	if err = token.Claims(matchingPublicKeys[0], target); err != nil {
		return fmt.Errorf("error decoding claims: %s", err)
	}

	return nil
}

func TestSign(t *testing.T) {
	b, storage := getTestBackend(t)

	role := "tester"

	if err := writeRole(b, storage, role, role+".example.com", map[string]interface{}{}); err != nil {
		t.Fatalf("%v\n", err)
	}

	claims := map[string]interface{}{
		"aud": []string{"Zapp Brannigan", "Kif Kroker"},
	}

	var decoded jwt.Claims
	if err := getSignedToken(b, storage, role, claims, &decoded); err != nil {
		t.Fatalf("%v\n", err)
	}

	expectedExpiry := jwt.NumericDate(3 * 60)
	expectedIssuedAt := jwt.NumericDate(0)
	expectedNotBefore := jwt.NumericDate(0)
	expectedClaims := jwt.Claims{
		Audience:  []string{"Zapp Brannigan", "Kif Kroker"},
		Expiry:    &expectedExpiry,
		IssuedAt:  &expectedIssuedAt,
		NotBefore: &expectedNotBefore,
		ID:        "1",
		Issuer:    role + ".example.com",
	}

	if diff := deep.Equal(expectedClaims, decoded); diff != nil {
		t.Error(diff)
	}
}

type customToken struct {
	Foo string `json:"foo"`
}

func TestPrivateClaim(t *testing.T) {
	b, storage := getTestBackend(t)

	if _, err := writeConfig(b, storage, map[string]interface{}{"allowed_claims": []string{"aud", "foo"}}); err != nil {
		t.Fatalf("%v\n", err)
	}

	role := "tester"

	if err := writeRole(b, storage, role, role+".example.com", map[string]interface{}{"aud": "an audience"}); err != nil {
		t.Fatalf("%v\n", err)
	}

	claims := map[string]interface{}{
		"foo": "bar",
	}

	var decoded customToken
	if err := getSignedToken(b, storage, role, claims, &decoded); err != nil {
		t.Fatalf("%v\n", err)
	}

	expectedClaims := customToken{
		Foo: "bar",
	}

	if diff := deep.Equal(expectedClaims, decoded); diff != nil {
		t.Error(diff)
	}
}

func TestRejectReservedClaims(t *testing.T) {
	b, storage := getTestBackend(t)

	role := "tester"

	if err := writeRole(b, storage, role, role+".example.com", map[string]interface{}{}); err != nil {
		t.Fatalf("%v\n", err)
	}

	data := map[string]interface{}{
		"claims": map[string]interface{}{
			"exp": 1234,
		},
	}

	req := &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "sign/" + role,
		Storage:   *storage,
		Data:      data,
	}

	resp, err := b.HandleRequest(context.Background(), req)
	if err == nil || resp != nil && !resp.IsError() {
		t.Fatalf("expected to get an error from sign. got:%v\n", resp)
	}
}

func TestRejectOverwriteRoleOtherClaim(t *testing.T) {
	b, storage := getTestBackend(t)

	role := "tester"

	if err := writeRole(b, storage, role, role+".example.com", map[string]interface{}{"aud": "an audience"}); err != nil {
		t.Fatalf("%v\n", err)
	}

	data := map[string]interface{}{
		"claims": map[string]interface{}{
			"aud": 1234,
		},
	}

	req := &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "sign/" + role,
		Storage:   *storage,
		Data:      data,
	}

	resp, err := b.HandleRequest(context.Background(), req)
	if err == nil || resp != nil && !resp.IsError() {
		t.Fatalf("expected to get an error from sign. got:%v\n", resp)
	}
}
