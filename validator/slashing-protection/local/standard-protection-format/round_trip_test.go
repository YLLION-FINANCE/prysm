package interchangeformat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	"github.com/prysmaticlabs/prysm/validator/db/kv"
	dbtest "github.com/prysmaticlabs/prysm/validator/db/testing"
	protectionFormat "github.com/prysmaticlabs/prysm/validator/slashing-protection/local/standard-protection-format"
	mocks "github.com/prysmaticlabs/prysm/validator/testing"
)

func TestImportExport_RoundTrip(t *testing.T) {
	ctx := context.Background()
	numValidators := 5
	publicKeys, err := mocks.CreateRandomPubKeys(numValidators)
	require.NoError(t, err)
	validatorDB := dbtest.SetupDB(t, publicKeys)

	// First we setup some mock attesting and proposal histories and create a mock
	// standard slashing protection format JSON struct.
	attestingHistory, proposalHistory, err := mocks.MockAttestingAndProposalHistories(numValidators)
	require.NoError(t, err)
	wanted, err := mocks.MockSlashingProtectionJSON(publicKeys, attestingHistory, proposalHistory)
	require.NoError(t, err)

	// We encode the standard slashing protection struct into a JSON format.
	blob, err := json.Marshal(wanted)
	require.NoError(t, err)
	buf := bytes.NewBuffer(blob)

	// Next, we attempt to import it into our validator database.
	err = protectionFormat.ImportStandardProtectionJSON(ctx, validatorDB, buf)
	require.NoError(t, err)

	// Next up, we export our slashing protection database into the EIP standard file.
	// Next, we attempt to import it into our validator database.
	eipStandard, err := protectionFormat.ExportStandardProtectionJSON(ctx, validatorDB)
	require.NoError(t, err)

	// We compare the metadata fields from import to export.
	require.Equal(t, wanted.Metadata, eipStandard.Metadata)

	// The values in the data field of the EIP struct are not guaranteed to be sorted,
	// so we create a map to verify we have the data we expected.
	require.Equal(t, len(wanted.Data), len(eipStandard.Data))

	dataByPubKey := make(map[string]*protectionFormat.ProtectionData)
	for _, item := range wanted.Data {
		dataByPubKey[item.Pubkey] = item
	}
	for _, item := range eipStandard.Data {
		want, ok := dataByPubKey[item.Pubkey]
		require.Equal(t, true, ok)
		require.DeepEqual(t, want, item)
	}
}

func TestImportInterchangeData_OK(t *testing.T) {
	ctx := context.Background()
	numValidators := 10
	publicKeys, err := mocks.CreateRandomPubKeys(numValidators)
	require.NoError(t, err)
	validatorDB := dbtest.SetupDB(t, publicKeys)

	// First we setup some mock attesting and proposal histories and create a mock
	// standard slashing protection format JSON struct.
	attestingHistory, proposalHistory, err := mocks.MockAttestingAndProposalHistories(numValidators)
	require.NoError(t, err)
	standardProtectionFormat, err := mocks.MockSlashingProtectionJSON(publicKeys, attestingHistory, proposalHistory)
	require.NoError(t, err)

	// We encode the standard slashing protection struct into a JSON format.
	blob, err := json.Marshal(standardProtectionFormat)
	require.NoError(t, err)
	buf := bytes.NewBuffer(blob)

	// Next, we attempt to import it into our validator database.
	err = protectionFormat.ImportStandardProtectionJSON(ctx, validatorDB, buf)
	require.NoError(t, err)

	// Next, we attempt to retrieve the attesting and proposals histories from our database and
	// verify those indeed match the originally generated mock histories.
	receivedAttestingHistory, err := validatorDB.AttestationHistoryForPubKeysV2(ctx, publicKeys)
	require.NoError(t, err)
	for i := 0; i < len(publicKeys); i++ {
		require.DeepEqual(
			t,
			attestingHistory[i],
			receivedAttestingHistory[publicKeys[i]],
			"We should have stored any attesting history",
		)
		proposals := proposalHistory[i].Proposals
		for _, proposal := range proposals {
			receivedProposalSigningRoot, _, err := validatorDB.ProposalHistoryForSlot(ctx, publicKeys[i], proposal.Slot)
			require.NoError(t, err)
			require.DeepEqual(
				t,
				receivedProposalSigningRoot[:],
				proposal.SigningRoot,
				"Imported proposals are different then the generated ones",
			)
		}
	}
}

func TestStore_ImportInterchangeData_BadFormat_PreventsDBWrites(t *testing.T) {
	ctx := context.Background()
	numValidators := 10
	publicKeys, err := mocks.CreateRandomPubKeys(numValidators)
	require.NoError(t, err)
	validatorDB := dbtest.SetupDB(t, publicKeys)

	// First we setup some mock attesting and proposal histories and create a mock
	// standard slashing protection format JSON struct.
	attestingHistory, proposalHistory, err := mocks.MockAttestingAndProposalHistories(numValidators)
	require.NoError(t, err)
	standardProtectionFormat, err := mocks.MockSlashingProtectionJSON(publicKeys, attestingHistory, proposalHistory)
	require.NoError(t, err)

	// We replace a slot of one of the blocks with junk data.
	standardProtectionFormat.Data[0].SignedBlocks[0].Slot = "BadSlot"

	// We encode the standard slashing protection struct into a JSON format.
	blob, err := json.Marshal(standardProtectionFormat)
	require.NoError(t, err)
	buf := bytes.NewBuffer(blob)

	// Next, we attempt to import it into our validator database and check that
	// we obtain an error during the import process.
	err = protectionFormat.ImportStandardProtectionJSON(ctx, validatorDB, buf)
	assert.NotNil(t, err)

	// Next, we attempt to retrieve the attesting and proposals histories from our database and
	// verify nothing was saved to the DB. If there is an error in the import process, we need to make
	// sure writing is an atomic operation: either the import succeeds and saves the slashing protection
	// data to our DB, or it does not.
	receivedAttestingHistory, err := validatorDB.AttestationHistoryForPubKeysV2(ctx, publicKeys)
	require.NoError(t, err)
	for i := 0; i < len(publicKeys); i++ {
		defaultAttestingHistory := kv.NewAttestationHistoryArray(0)
		require.DeepEqual(
			t,
			defaultAttestingHistory,
			receivedAttestingHistory[publicKeys[i]],
			"Imported attestation protection history is different than the empty default",
		)
		proposals := proposalHistory[i].Proposals
		for _, proposal := range proposals {
			receivedProposalSigningRoot, _, err := validatorDB.ProposalHistoryForSlot(ctx, publicKeys[i], proposal.Slot)
			require.NoError(t, err)
			require.DeepEqual(
				t,
				params.BeaconConfig().ZeroHash,
				receivedProposalSigningRoot,
				"Imported proposal signing root is different than the empty default",
			)
		}
	}
}