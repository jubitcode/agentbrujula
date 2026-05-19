package remote

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoteManagerNew(t *testing.T) {
	rm := NewRemoteManager()
	assert.NotNil(t, rm)
}

func TestRemoteConfigNew(t *testing.T) {
	config := &RemoteConfig{
		Name: "prod",
		Host: "192.168.1.100",
		User: "deploy",
		Port: 22,
	}

	assert.Equal(t, "prod", config.Name)
	assert.Equal(t, "192.168.1.100", config.Host)
	assert.Equal(t, "deploy", config.User)
	assert.Equal(t, 22, config.Port)
}

func TestRemoteConfigAddress(t *testing.T) {
	config := &RemoteConfig{
		Host: "192.168.1.100",
		User: "deploy",
		Port: 22,
	}

	assert.Equal(t, "deploy@192.168.1.100:22", config.Address())
}

func TestRemoteConfigAddressDefaultPort(t *testing.T) {
	config := &RemoteConfig{
		Host: "192.168.1.100",
		User: "deploy",
	}

	assert.Equal(t, "deploy@192.168.1.100:22", config.Address())
}

func TestRemoteConfigSSHTarget(t *testing.T) {
	config := &RemoteConfig{
		Host: "192.168.1.100",
		User: "deploy",
	}

	assert.Equal(t, "deploy@192.168.1.100", config.SSHTarget())
}

func TestRemoteConfigValidate(t *testing.T) {
	valid := &RemoteConfig{
		Name: "prod",
		Host: "192.168.1.100",
		User: "deploy",
	}

	assert.NoError(t, valid.Validate())

	noHost := &RemoteConfig{Name: "test", User: "deploy"}
	assert.Error(t, noHost.Validate())

	noUser := &RemoteConfig{Name: "test", Host: "192.168.1.100"}
	assert.Error(t, noUser.Validate())

	noName := &RemoteConfig{Host: "192.168.1.100", User: "deploy"}
	assert.Error(t, noName.Validate())
}

func TestRemoteStoreNew(t *testing.T) {
	store := NewRemoteStore(t.TempDir())
	assert.NotNil(t, store)
}

func TestRemoteStoreAddGet(t *testing.T) {
	store := NewRemoteStore(t.TempDir())

	config := &RemoteConfig{
		Name: "prod",
		Host: "192.168.1.100",
		User: "deploy",
	}

	err := store.Add(config)
	assert.NoError(t, err)

	got, err := store.Get("prod")
	assert.NoError(t, err)
	assert.Equal(t, "192.168.1.100", got.Host)
}

func TestRemoteStoreGetNotFound(t *testing.T) {
	store := NewRemoteStore(t.TempDir())

	_, err := store.Get("nonexistent")
	assert.Error(t, err)
}

func TestRemoteStoreList(t *testing.T) {
	store := NewRemoteStore(t.TempDir())

	store.Add(&RemoteConfig{Name: "prod", Host: "1.1.1.1", User: "deploy"})
	store.Add(&RemoteConfig{Name: "staging", Host: "2.2.2.2", User: "deploy"})

	list, err := store.List()
	assert.NoError(t, err)
	assert.Len(t, list, 2)
}

func TestRemoteStoreRemove(t *testing.T) {
	store := NewRemoteStore(t.TempDir())

	store.Add(&RemoteConfig{Name: "prod", Host: "1.1.1.1", User: "deploy"})

	err := store.Remove("prod")
	assert.NoError(t, err)

	_, err = store.Get("prod")
	assert.Error(t, err)
}

func TestSyncOptionsStruct(t *testing.T) {
	opts := &SyncOptions{
		Exclude: []string{"*.log", "tmp/"},
		Delete:  true,
		DryRun:  true,
	}

	assert.Contains(t, opts.Exclude, "*.log")
	assert.Contains(t, opts.Exclude, "tmp/")
	assert.True(t, opts.Delete)
	assert.True(t, opts.DryRun)
}

func TestSyncOptionsDefaults(t *testing.T) {
	opts := &SyncOptions{}

	assert.Nil(t, opts.Exclude)
	assert.False(t, opts.Delete)
	assert.False(t, opts.DryRun)
}

func TestSyncStatusNew(t *testing.T) {
	status := &SyncStatus{
		InSync:       true,
		LocalChanges: 0,
	}

	assert.True(t, status.InSync)
	assert.Equal(t, 0, status.LocalChanges)
}

func TestSyncStatusOutOfSync(t *testing.T) {
	status := &SyncStatus{
		InSync:        false,
		LocalChanges:  5,
		RemoteChanges: 2,
	}

	assert.False(t, status.InSync)
	assert.Equal(t, 5, status.LocalChanges)
	assert.Equal(t, 2, status.RemoteChanges)
}

func TestRemoteConnectionStateNew(t *testing.T) {
	state := &ConnectionState{
		Connected: false,
	}

	assert.False(t, state.Connected)
}

func TestRemoteConnectionStateConnected(t *testing.T) {
	state := &ConnectionState{
		Connected:  true,
		RemoteName: "prod",
	}

	assert.True(t, state.Connected)
	assert.Equal(t, "prod", state.RemoteName)
}
