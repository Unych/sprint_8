package main

import (
	"database/sql"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	data, err := os.ReadFile("tracker.db")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "tracker.db")
	require.NoError(t, os.WriteFile(path, data, 0600))
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	return db
}

var (
	// randSource источник псевдо случайных чисел.
	// Для повышения уникальности в качестве seed
	// используется текущее время в unix формате (в виде числа)
	randSource = rand.NewSource(time.Now().UnixNano())
	// randRange использует randSource для генерации случайных чисел
	randRange = rand.New(randSource)
)

// getTestParcel возвращает тестовую посылку
func getTestParcel() Parcel {
	return Parcel{
		Client:    1000,
		Status:    ParcelStatusRegistered,
		Address:   "test",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// TestAddGetDelete проверяет добавление, получение и удаление посылки
func TestAddGetDelete(t *testing.T) {
	// prepare
	db := openTestDB(t)
	store := NewParcelStore(db)
	parcel := getTestParcel()

	// add
	id, err := store.Add(parcel)
	require.NoError(t, err)
	require.Positive(t, id)
	parcel.Number = id

	// get
	stored, err := store.Get(id)
	require.NoError(t, err)
	require.Equal(t, parcel, stored)

	// delete
	require.NoError(t, store.Delete(id))
	_, err = store.Get(id)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

// TestSetAddress проверяет обновление адреса
func TestSetAddress(t *testing.T) {
	// prepare
	db := openTestDB(t)
	store := NewParcelStore(db)
	parcel := getTestParcel()

	// add
	id, err := store.Add(parcel)
	require.NoError(t, err)
	require.Positive(t, id)
	parcel.Number = id

	// set address
	newAddress := "new test address"
	require.NoError(t, store.SetAddress(id, newAddress))

	// check
	stored, err := store.Get(id)
	require.NoError(t, err)
	parcel.Address = newAddress
	require.Equal(t, parcel, stored)
}

// TestSetStatus проверяет обновление статуса
func TestSetStatus(t *testing.T) {
	// prepare
	db := openTestDB(t)
	store := NewParcelStore(db)
	parcel := getTestParcel()

	// add
	id, err := store.Add(parcel)
	require.NoError(t, err)
	require.Positive(t, id)
	parcel.Number = id

	// set status
	for _, status := range []string{ParcelStatusSent, ParcelStatusDelivered} {
		require.NoError(t, store.SetStatus(id, status))

		// check
		stored, err := store.Get(id)
		require.NoError(t, err)
		parcel.Status = status
		require.Equal(t, parcel, stored)
	}
}

// TestGetByClient проверяет получение посылок по идентификатору клиента
func TestGetByClient(t *testing.T) {
	// prepare
	db := openTestDB(t)
	store := NewParcelStore(db)
	_, err := db.Exec("DELETE FROM parcel")
	require.NoError(t, err)

	parcels := []Parcel{
		getTestParcel(),
		getTestParcel(),
		getTestParcel(),
	}
	parcelMap := map[int]Parcel{}

	// задаём всем посылкам один и тот же идентификатор клиента
	client := randRange.Intn(10_000_000)
	parcels[0].Client = client
	parcels[1].Client = client
	parcels[2].Client = client

	// add
	for i := 0; i < len(parcels); i++ {
		id, err := store.Add(parcels[i])
		require.NoError(t, err)
		require.Positive(t, id)

		// обновляем идентификатор добавленной у посылки
		parcels[i].Number = id

		// сохраняем добавленную посылку в структуру map, чтобы её можно было легко достать по идентификатору посылки
		parcelMap[id] = parcels[i]
	}

	// get by client
	other := getTestParcel()
	other.Client = client + 1
	_, err = store.Add(other)
	require.NoError(t, err)

	storedParcels, err := store.GetByClient(client)
	require.NoError(t, err)
	require.Len(t, storedParcels, len(parcels))

	// check
	for _, parcel := range storedParcels {
		require.Contains(t, parcelMap, parcel.Number)
		require.Equal(t, parcelMap[parcel.Number], parcel)
	}
	storedParcels, err = store.GetByClient(client + 2)
	require.NoError(t, err)
	require.Empty(t, storedParcels)
}

func TestSentAndDeliveredParcelsCannotBeChangedOrDeleted(t *testing.T) {
	for _, status := range []string{ParcelStatusSent, ParcelStatusDelivered} {
		t.Run(status, func(t *testing.T) {
			store := NewParcelStore(openTestDB(t))
			parcel := getTestParcel()
			parcel.Status = status
			id, err := store.Add(parcel)
			require.NoError(t, err)
			parcel.Number = id

			require.NoError(t, store.SetAddress(id, "another address"))
			stored, err := store.Get(id)
			require.NoError(t, err)
			require.Equal(t, parcel, stored)

			require.NoError(t, store.Delete(id))
			stored, err = store.Get(id)
			require.NoError(t, err)
			require.Equal(t, parcel, stored)
		})
	}
}

func TestParcelServiceRegisterAndNextStatus(t *testing.T) {
	store := NewParcelStore(openTestDB(t))
	service := NewParcelService(store)
	parcel, err := service.Register(1000, "test address")
	require.NoError(t, err)
	require.Positive(t, parcel.Number)
	require.Equal(t, ParcelStatusRegistered, parcel.Status)
	_, err = time.Parse(time.RFC3339, parcel.CreatedAt)
	require.NoError(t, err)
	stored, err := store.Get(parcel.Number)
	require.NoError(t, err)
	require.Equal(t, parcel, stored)

	for _, status := range []string{ParcelStatusSent, ParcelStatusDelivered, ParcelStatusDelivered} {
		require.NoError(t, service.NextStatus(parcel.Number))
		stored, err := store.Get(parcel.Number)
		require.NoError(t, err)
		require.Equal(t, status, stored.Status)
	}
}
