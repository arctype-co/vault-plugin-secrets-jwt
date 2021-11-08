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
	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"
	"path"
	"regexp"
)

const (
	keyStorageRolePath = "role"
	keyRoleName        = "name"
	keyIssuer          = "issuer"
)

type Role struct {

	// Issuer defines the 'iss' claim for the issued JWT. It is required for each role.
	Issuer string

	// Claims defines claim values to be set on the issued JWT; each claim must be allowed by the plugin config.
	Claims map[string]interface{} `json:"claims"`

	// SubjectPattern defines a regular expression (https://golang.org/pkg/regexp/) which must be matched by any
	// incoming 'sub' claims. This restriction is in addition to that defined on the plugin config.
	SubjectPattern *regexp.Regexp

	// AudiencePattern defines a regular expression (https://golang.org/pkg/regexp/) which must be matched by any
	// incoming 'aud' claims. If the audience claim is an array, each element in the array must match the pattern.
	// This restriction is in addition to that defined on the plugin config.
	AudiencePattern *regexp.Regexp
}

// Return response data for a role
func (r *Role) toResponseData() map[string]interface{} {
	respData := map[string]interface{}{
		keyIssuer:              r.Issuer,
		keyClaims:              r.Claims,
		keySubjectPattern:      r.SubjectPattern,
		keyAudiencePattern:     r.AudiencePattern,
	}
	return respData
}

func pathRole(b *backend) []*framework.Path {
	return []*framework.Path{
		{
			Pattern: "roles/" + framework.GenericNameRegex(keyRoleName),
			Fields: map[string]*framework.FieldSchema{
				keyRoleName: {
					Type:        framework.TypeLowerCaseString,
					Description: `Specifies the name of the role to create. This is part of the request URL.`,
					Required:    true,
				},
				keyIssuer: {
					Type:        framework.TypeString,
					Description: `Value to set as the 'iss' claim. Required on all roles.`,
				},
				keyClaims: {
					Type:        framework.TypeMap,
					Description: `Claims to be set on issued JWTs. Each claim must be allowed by the configuration.`,
				},
				keySubjectPattern: {
					Type:        framework.TypeString,
					Description: `Regular expression which must match 'sub' claims provided during sign requests.
This restriction is in addition to that defined in the config.`,
				},
				keyAudiencePattern: {
					Type:        framework.TypeString,
					Description: `Regular expression which must match 'aud' claims provided during sign requests.
This restriction is in addition to that defined in the config.`,
				},
				keyMaxAllowedAudiences: {
					Type:        framework.TypeInt,
					Description: `Maximum number of allowed audiences, or -1 for no limit.
Must be less than or equal to the maximum number of allowed audiences defined in the config`,
				},
				keyAllowedClaims: {
					Type: framework.TypeStringSlice,
					Description: `Claims which are able to be set in addition to ones generated by the backend.
Note: 'aud' and 'sub' should be in this list if you would like to set them.`,
				},
			},
			Operations: map[logical.Operation]framework.OperationHandler{
				logical.ReadOperation: &framework.PathOperation{
					Callback: b.pathRolesRead,
				},
				logical.CreateOperation: &framework.PathOperation{
					Callback: b.pathRolesWrite,
				},
				logical.UpdateOperation: &framework.PathOperation{
					Callback: b.pathRolesWrite,
				},
				logical.DeleteOperation: &framework.PathOperation{
					Callback: b.pathRolesDelete,
				},
			},
			ExistenceCheck:  b.pathRoleExistenceCheck,
			HelpSynopsis:    pathRoleHelpSyn,
			HelpDescription: pathRoleHelpDesc,
		},
		{
			Pattern: "roles/?$",
			Operations: map[logical.Operation]framework.OperationHandler{
				logical.ListOperation: &framework.PathOperation{
					Callback: b.pathRolesList,
				},
			},
			HelpSynopsis:    pathRoleListHelpSyn,
			HelpDescription: pathRoleListHelpDesc,
		},
	}
}

func (b *backend) pathRoleExistenceCheck(ctx context.Context, req *logical.Request, d *framework.FieldData) (bool, error) {
	name := d.Get("name").(string)

	role, err := req.Storage.Get(ctx, path.Join(keyStorageRolePath, name))
	if err != nil {
		return false, err
	}

	return role != nil, nil
}

// pathRolesList makes a request to Vault storage to retrieve a list of roles for the backend
func (b *backend) pathRolesList(ctx context.Context, req *logical.Request, _ *framework.FieldData) (*logical.Response, error) {
	entries, err := req.Storage.List(ctx, keyStorageRolePath+"/")
	if err != nil {
		return nil, err
	}

	return logical.ListResponse(entries), nil
}

// pathRolesRead makes a request to Vault storage to read a role and return response data
func (b *backend) pathRolesRead(ctx context.Context, req *logical.Request, d *framework.FieldData) (*logical.Response, error) {
	role, err := b.getRole(ctx, req.Storage, d.Get(keyRoleName).(string))
	if err != nil {
		return nil, err
	}

	if role == nil {
		return nil, nil
	}

	return &logical.Response{
		Data: role.toResponseData(),
	}, nil
}

// pathRolesWrite makes a request to Vault storage to update a role based on the attributes passed to the role configuration
func (b *backend) pathRolesWrite(ctx context.Context, req *logical.Request, d *framework.FieldData) (*logical.Response, error) {
	name, ok := d.GetOk(keyRoleName)
	if !ok {
		return logical.ErrorResponse("missing role name"), nil
	}

	role, err := b.getRole(ctx, req.Storage, name.(string))
	if err != nil {
		return nil, err
	}

	if role == nil {
		role = &Role{}
	}

	config, err := b.getConfig(ctx, req.Storage)
	if err != nil {
		return nil, err
	}

	createOperation := req.Operation == logical.CreateOperation

	if newIssuer, ok := d.GetOk(keyIssuer); ok {
		role.Issuer = newIssuer.(string)
	} else if !ok && createOperation {
		return nil, fmt.Errorf("missing issuer in role")
	}

	if newClaims, ok := d.GetOk(keyClaims); ok {
		role.Claims = newClaims.(map[string]interface{})
	}

	if newAudiencePattern, ok := d.GetOk(keyAudiencePattern); ok {
		pattern, err := regexp.Compile(newAudiencePattern.(string))
		if err != nil {
			return nil, err
		}
		role.AudiencePattern = pattern
	}

	if newSubjectPattern, ok := d.GetOk(keySubjectPattern); ok {
		pattern, err := regexp.Compile(newSubjectPattern.(string))
		if err != nil {
			return nil, err
		}
		role.SubjectPattern = pattern
	}

	// Check any provided claims are allowed from the config.
	for claim := range role.Claims {
		if allowedClaim, ok := config.allowedClaimsMap[claim]; !ok || !allowedClaim {
			return logical.ErrorResponse("claim %s not permitted", claim), logical.ErrInvalidRequest
		}
	}

	// Check that issuer claim isn't included in claims field.
	if _, ok := role.Claims["iss"]; ok {
		return logical.ErrorResponse("'iss' claim cannot be present in 'claims' field"), logical.ErrInvalidRequest
	}

	// Check that subject claim isn't included in claims field.
	if _, ok := role.Claims["sub"]; ok {
		return logical.ErrorResponse("'sub' claim cannot be present in 'claims' field"), logical.ErrInvalidRequest
	}

	// If any audience is set in the claims, validate it against the configured restrictions.
	if rawAud, ok := role.Claims["aud"]; ok {
		switch aud := rawAud.(type) {
		case string:
			if !config.AudiencePattern.MatchString(aud) {
				return logical.ErrorResponse("validation of 'aud' claim failed"), logical.ErrInvalidRequest
			}
		case []string:
			if config.MaxAudiences > -1 && len(aud) > config.MaxAudiences {
				return logical.ErrorResponse("too many audience claims: %d", len(aud)), logical.ErrInvalidRequest
			}
			for _, audEntry := range aud {
				if !config.AudiencePattern.MatchString(audEntry) {
					return logical.ErrorResponse("validation of 'aud' claim failed"), logical.ErrInvalidRequest
				}
			}
		default:
			return logical.ErrorResponse("'aud' claim was %T, not string or []string", rawAud), logical.ErrInvalidRequest
		}
	}

	if err := b.setRole(ctx, req.Storage, name.(string), role); err != nil {
		return nil, err
	}

	return nil, nil
}

// pathRolesDelete makes a request to Vault storage to delete a role
func (b *backend) pathRolesDelete(ctx context.Context, req *logical.Request, d *framework.FieldData) (*logical.Response, error) {
	err := req.Storage.Delete(ctx, path.Join(keyStorageRolePath, d.Get(keyRoleName).(string)))
	if err != nil {
		return nil, fmt.Errorf("error deleting role: %w", err)
	}
	return nil, nil
}

// getRole gets the role from the Vault storage API
func (b *backend) getRole(ctx context.Context, stg logical.Storage, name string) (*Role, error) {
	if name == "" {
		return nil, fmt.Errorf("missing role name")
	}

	entry, err := stg.Get(ctx, path.Join(keyStorageRolePath, name))
	if err != nil {
		return nil, err
	}

	if entry == nil {
		return nil, nil
	}

	var role Role

	if err := entry.DecodeJSON(&role); err != nil {
		return nil, err
	}
	return &role, nil
}

// setRole adds the role to the Vault storage API
func (b *backend) setRole(ctx context.Context, stg logical.Storage, name string, role *Role) error {
	entry, err := logical.StorageEntryJSON(path.Join(keyStorageRolePath, name), role)
	if err != nil {
		return err
	}

	if entry == nil {
		return fmt.Errorf("failed to create storage entry for role")
	}

	if err := stg.Put(ctx, entry); err != nil {
		return err
	}

	return nil
}

const pathRoleHelpSyn = `
Manages Vault role for generating tokens.
`

const pathRoleHelpDesc = `
Manages Vault role for generating tokens.

subject:          Subject claim (sub) for tokens generated using this role.
`

const pathRoleListHelpSyn = `
This endpoint returns a list of available roles.
`

const pathRoleListHelpDesc = `
This endpoint returns a list of available roles. Only the role names are returned, not any values.
`
