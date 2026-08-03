package syncpg

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

const ownerOne = "11111111-1111-1111-1111-111111111111"

func schemaConn(t *testing.T) (context.Context, *pgx.Conn) {
	t.Helper()
	ctx, conn := context.Background(), testConn(t)
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO owners(id) VALUES ($1)", ownerOne); err != nil {
		t.Fatal(err)
	}
	return ctx, conn
}

func TestSchemaOwnerSessionAndCanonicalConstraints(t *testing.T) {
	ctx, conn := schemaConn(t)
	for _, statement := range []string{
		"INSERT INTO projects(owner_id,id,version,payload) VALUES ($1,'one',1,'{}'),($1,'two',1,'{}')",
		"INSERT INTO sessions(owner_id,id,project_id,version,payload) VALUES ($1,'s','one',1,'{}')",
	} {
		if _, err := conn.Exec(ctx, statement, ownerOne); err != nil {
			t.Fatal(err)
		}
	}
	for name, statement := range map[string]string{
		"credential hash":   "INSERT INTO devices(id,owner_id,display_name,credential_hash,credential_prefix) VALUES ('eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee',$1,'d',decode('00','hex'),'p')",
		"lifecycle":         "INSERT INTO observations(owner_id,id,project_id,scope,type,title,content,lifecycle,review_state,version) VALUES ($1,'bad','one','project','t','t','c','bad','clear',1)",
		"version":           "INSERT INTO projects(owner_id,id,version,payload) VALUES ($1,'bad',-1,'{}')",
		"duplicate session": "INSERT INTO sessions(owner_id,id,project_id,version,payload) VALUES ($1,'s','two',1,'{}')",
		"session project":   "INSERT INTO observations(owner_id,id,project_id,session_id,scope,type,title,content,lifecycle,review_state,version) VALUES ($1,'o','two','s','project','t','t','c','active','clear',1)",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := conn.Exec(ctx, statement, ownerOne); err == nil {
				t.Fatal("invalid row accepted")
			}
		})
	}
	if _, err := conn.Exec(ctx, "INSERT INTO devices(id,owner_id,display_name,credential_hash,credential_prefix) VALUES ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb',$1,'d',decode(repeat('00',32),'hex'),'p')", ownerOne); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO mutations(owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition) VALUES ($1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','cccccccc-cccc-cccc-cccc-cccccccccccc',decode('00','hex'),'create','r',0,'accepted')", ownerOne); err == nil {
		t.Fatal("canonical disposition accepted without sequence")
	}
}

func TestSchemaBindsObservationConflictHistory(t *testing.T) {
	ctx, conn := schemaConn(t)
	for _, statement := range []string{
		"INSERT INTO projects(owner_id,id,version,payload) VALUES ($1,'p',1,'{}')",
		"INSERT INTO observations(owner_id,id,project_id,scope,type,title,content,lifecycle,review_state,version) VALUES ($1,'o','p','project','t','t','c','active','clear',1)",
		"INSERT INTO devices(id,owner_id,display_name,credential_hash,credential_prefix) VALUES ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb',$1,'d',decode(repeat('00',32),'hex'),'p')",
		"INSERT INTO mutations(owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition,canonical_seq) VALUES ($1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','cccccccc-cccc-cccc-cccc-cccccccccccc',decode('00','hex'),'create','o',0,'conflict',1),($1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','dddddddd-dddd-dddd-dddd-dddddddddddd',decode('00','hex'),'resolve','o',0,'accepted',2),($1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee',decode('00','hex'),'create','o',0,'conflict',3),($1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','ffffffff-ffff-ffff-ffff-ffffffffffff',decode('00','hex'),'create','p',0,'accepted',4)",
		"INSERT INTO changes(owner_id,seq,mutation_device_id,mutation_id,change_kind,record_kind,record_id) VALUES ($1,1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','cccccccc-cccc-cccc-cccc-cccccccccccc','conflict','observation','o'),($1,2,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','dddddddd-dddd-dddd-dddd-dddddddddddd','accepted','observation','o'),($1,3,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee','conflict','observation','o')",
		"INSERT INTO record_versions(owner_id,record_kind,record_id,record_version,source_device_id,source_mutation_id,base_version,disposition,snapshot) VALUES ($1,'observation','o',1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','cccccccc-cccc-cccc-cccc-cccccccccccc',0,'conflict','{}'),($1,'project','p',1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','ffffffff-ffff-ffff-ffff-ffffffffffff',0,'accepted','{}')",
	} {
		if _, err := conn.Exec(ctx, statement, ownerOne); err != nil {
			t.Fatal(err)
		}
	}
	for name, statement := range map[string]string{
		"competing": "INSERT INTO observation_conflicts(owner_id,conflict_id,observation_id,canonical_version,competing_version_id,status,created_seq) VALUES ($1,'11111111-1111-1111-1111-111111111112','o',1,2,'unresolved',1)",
		"created":   "INSERT INTO observation_conflicts(owner_id,conflict_id,observation_id,canonical_version,competing_version_id,status,created_seq) VALUES ($1,'11111111-1111-1111-1111-111111111113','o',1,1,'unresolved',2)",
		"resolved":  "INSERT INTO observation_conflicts(owner_id,conflict_id,observation_id,canonical_version,competing_version_id,status,created_seq,resolved_seq) VALUES ($1,'11111111-1111-1111-1111-111111111114','o',1,1,'resolved',1,1)",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := conn.Exec(ctx, statement, ownerOne); err == nil {
				t.Fatal("mismatched conflict history accepted")
			}
		})
	}
	for _, statement := range []string{
		"INSERT INTO observation_conflicts(owner_id,conflict_id,observation_id,canonical_version,competing_version_id,status,created_seq) VALUES ($1,'11111111-1111-1111-1111-111111111110','o',1,1,'unresolved',1)",
		"INSERT INTO observation_conflicts(owner_id,conflict_id,observation_id,canonical_version,competing_version_id,status,created_seq,resolved_seq) VALUES ($1,'11111111-1111-1111-1111-111111111111','o',1,1,'resolved',1,2)",
	} {
		if _, err := conn.Exec(ctx, statement, ownerOne); err != nil {
			t.Fatalf("valid conflict: %v", err)
		}
	}
	if _, err := conn.Exec(ctx, "INSERT INTO observation_conflicts(owner_id,conflict_id,observation_id,canonical_version,competing_version_id,status,created_seq,resolved_seq) VALUES ($1,'11111111-1111-1111-1111-111111111115','o',1,1,'resolved',3,2)", ownerOne); err == nil {
		t.Fatal("out-of-order resolution accepted")
	}
}

func TestSchemaBindsChangeAndTombstoneLinks(t *testing.T) {
	ctx, conn := schemaConn(t)
	for _, statement := range []string{
		"INSERT INTO devices(id,owner_id,display_name,credential_hash,credential_prefix) VALUES ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb',$1,'d',decode(repeat('00',32),'hex'),'p')",
		"INSERT INTO mutations(owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition,canonical_seq) VALUES ($1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','cccccccc-cccc-cccc-cccc-cccccccccccc',decode('00','hex'),'create','one',0,'accepted',1),($1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','dddddddd-dddd-dddd-dddd-dddddddddddd',decode('00','hex'),'create','two',0,'accepted',2)",
		"INSERT INTO record_versions(owner_id,record_kind,record_id,record_version,source_device_id,source_mutation_id,base_version,disposition,snapshot) VALUES ($1,'project','one',0,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','cccccccc-cccc-cccc-cccc-cccccccccccc',0,'accepted','{}'),($1,'project','two',0,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','dddddddd-dddd-dddd-dddd-dddddddddddd',0,'accepted','{}')",
	} {
		if _, err := conn.Exec(ctx, statement, ownerOne); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		"INSERT INTO changes(owner_id,seq,mutation_device_id,mutation_id,change_kind,record_kind,record_id,version_id) VALUES ($1,1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','cccccccc-cccc-cccc-cccc-cccccccccccc','accepted','project','one',2)",
		"INSERT INTO tombstones(owner_id,record_kind,record_id,version_id,mutation_device_id,mutation_id) VALUES ($1,'project','one',2,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','cccccccc-cccc-cccc-cccc-cccccccccccc')",
	} {
		if _, err := conn.Exec(ctx, statement, ownerOne); err == nil {
			t.Fatal("mismatched link accepted")
		}
	}
}

func TestSchemaBindsChangeMutation(t *testing.T) {
	ctx, conn := schemaConn(t)
	for _, statement := range []string{
		"INSERT INTO devices(id,owner_id,display_name,credential_hash,credential_prefix) VALUES ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb',$1,'d',decode(repeat('00',32),'hex'),'p')",
		"INSERT INTO mutations(owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition,canonical_seq) VALUES ($1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','cccccccc-cccc-cccc-cccc-cccccccccccc',decode('00','hex'),'create','one',0,'accepted',1)",
	} {
		if _, err := conn.Exec(ctx, statement, ownerOne); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		"INSERT INTO changes(owner_id,seq,mutation_device_id,mutation_id,change_kind,record_kind,record_id) VALUES ($1,1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','cccccccc-cccc-cccc-cccc-cccccccccccc','accepted','project','two')",
		"INSERT INTO changes(owner_id,seq,mutation_device_id,mutation_id,change_kind,record_kind,record_id) VALUES ($1,2,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','cccccccc-cccc-cccc-cccc-cccccccccccc','conflict','project','one')",
	} {
		if _, err := conn.Exec(ctx, statement, ownerOne); err == nil {
			t.Fatal("mismatched mutation accepted")
		}
	}
}

func TestSchemaBindsRecordVersionSourceMutation(t *testing.T) {
	ctx, conn := schemaConn(t)
	for _, statement := range []string{
		"INSERT INTO devices(id,owner_id,display_name,credential_hash,credential_prefix) VALUES ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb',$1,'d',decode(repeat('00',32),'hex'),'p')",
		"INSERT INTO mutations(owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition,canonical_seq) VALUES ($1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','cccccccc-cccc-cccc-cccc-cccccccccccc',decode('00','hex'),'create','one',0,'accepted',1),($1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','dddddddd-dddd-dddd-dddd-dddddddddddd',decode('00','hex'),'create','two',0,'conflict',2)",
	} {
		if _, err := conn.Exec(ctx, statement, ownerOne); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		"INSERT INTO record_versions(owner_id,record_kind,record_id,record_version,source_device_id,source_mutation_id,base_version,disposition,snapshot) VALUES ($1,'project','one',1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','dddddddd-dddd-dddd-dddd-dddddddddddd',0,'conflict','{}')",
		"INSERT INTO record_versions(owner_id,record_kind,record_id,record_version,source_device_id,source_mutation_id,base_version,disposition,snapshot) VALUES ($1,'project','two',1,'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','dddddddd-dddd-dddd-dddd-dddddddddddd',0,'accepted','{}')",
	} {
		if _, err := conn.Exec(ctx, statement, ownerOne); err == nil {
			t.Fatal("mismatched source mutation accepted")
		}
	}
}
