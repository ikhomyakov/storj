// Copyright (C) 2020 Storj Labs, Inc.
// See LICENSE for copying information.

package metabase

import (
	"context"
	"database/sql"
	"errors"

	"storj.io/common/storj"
	"storj.io/common/uuid"
)

// Object object metadata.
// TODO define separated struct.
type Object RawObject

// Segment segment metadata.
// TODO define separated struct.
type Segment RawSegment

// GetObjectExactVersion contains arguments necessary for fetching an information
// about exact object version.
type GetObjectExactVersion struct {
	Version Version
	ObjectLocation
}

// Verify verifies get object reqest fields.
func (obj *GetObjectExactVersion) Verify() error {
	if err := obj.ObjectLocation.Verify(); err != nil {
		return err
	}
	if obj.Version <= 0 {
		return ErrInvalidRequest.New("Version invalid: %v", obj.Version)
	}
	return nil
}

// GetObjectExactVersion returns object information for exact version.
func (db *DB) GetObjectExactVersion(ctx context.Context, opts GetObjectExactVersion) (_ Object, err error) {
	defer mon.Task()(&ctx)(&err)

	if err := opts.Verify(); err != nil {
		return Object{}, err
	}

	object := Object{}
	// TODO handle encryption column
	err = db.db.QueryRow(ctx, `
		SELECT
			stream_id,
			created_at, expires_at,
			segment_count,
			encrypted_metadata_nonce, encrypted_metadata, encrypted_metadata_encrypted_key,
			total_encrypted_size, fixed_segment_size,
			encryption
		FROM objects
		WHERE
			project_id   = $1 AND
			bucket_name  = $2 AND
			object_key   = $3 AND
			version      = $4 AND
			status       = `+committedStatus,
		opts.ProjectID, opts.BucketName, []byte(opts.ObjectKey), opts.Version).
		Scan(
			&object.StreamID,
			&object.CreatedAt, &object.ExpiresAt,
			&object.SegmentCount,
			&object.EncryptedMetadataNonce, &object.EncryptedMetadata, &object.EncryptedMetadataEncryptedKey,
			&object.TotalEncryptedSize, &object.FixedSegmentSize,
			encryptionParameters{&object.Encryption},
		)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Object{}, storj.ErrObjectNotFound.Wrap(Error.Wrap(err))
		}
		return Object{}, Error.New("unable to query object status: %w", err)
	}

	object.ProjectID = opts.ProjectID
	object.BucketName = opts.BucketName
	object.ObjectKey = opts.ObjectKey
	object.Version = opts.Version

	object.Status = Committed

	return object, nil
}

// GetObjectLatestVersion contains arguments necessary for fetching
// an object information for latest version.
type GetObjectLatestVersion struct {
	ObjectLocation
}

// GetObjectLatestVersion returns object information for latest version.
func (db *DB) GetObjectLatestVersion(ctx context.Context, opts GetObjectLatestVersion) (_ Object, err error) {
	defer mon.Task()(&ctx)(&err)

	if err := opts.Verify(); err != nil {
		return Object{}, err
	}

	object := Object{}
	err = db.db.QueryRow(ctx, `
		SELECT
			stream_id, version,
			created_at, expires_at,
			segment_count,
			encrypted_metadata_nonce, encrypted_metadata, encrypted_metadata_encrypted_key,
			total_encrypted_size, fixed_segment_size,
			encryption
		FROM objects
		WHERE
			project_id   = $1 AND
			bucket_name  = $2 AND
			object_key   = $3 AND
			status       = `+committedStatus+`
		ORDER BY version desc
		LIMIT 1
	`, opts.ProjectID, opts.BucketName, []byte(opts.ObjectKey)).
		Scan(
			&object.StreamID, &object.Version,
			&object.CreatedAt, &object.ExpiresAt,
			&object.SegmentCount,
			&object.EncryptedMetadataNonce, &object.EncryptedMetadata, &object.EncryptedMetadataEncryptedKey,
			&object.TotalEncryptedSize, &object.FixedSegmentSize,
			encryptionParameters{&object.Encryption},
		)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Object{}, storj.ErrObjectNotFound.Wrap(Error.Wrap(err))
		}
		return Object{}, Error.New("unable to query object status: %w", err)
	}

	object.ProjectID = opts.ProjectID
	object.BucketName = opts.BucketName
	object.ObjectKey = opts.ObjectKey

	object.Status = Committed

	return object, nil
}

// GetSegmentByPosition contains arguments necessary for fetching a segment on specific position.
type GetSegmentByPosition struct {
	StreamID uuid.UUID
	Position SegmentPosition
}

// Verify verifies get segment request fields.
func (seg *GetSegmentByPosition) Verify() error {
	if seg.StreamID.IsZero() {
		return ErrInvalidRequest.New("StreamID missing")
	}
	return nil
}

// GetSegmentByPosition returns a information about segment which covers specified offset.
func (db *DB) GetSegmentByPosition(ctx context.Context, opts GetSegmentByPosition) (segment Segment, err error) {
	defer mon.Task()(&ctx)(&err)

	if err := opts.Verify(); err != nil {
		return Segment{}, err
	}

	err = db.db.QueryRow(ctx, `
		SELECT
			root_piece_id, encrypted_key_nonce, encrypted_key,
			encrypted_size, plain_offset, plain_size,
			redundancy,
			inline_data, remote_pieces
		FROM objects, segments
		WHERE
			segments.stream_id = $1 AND
			segments.position  = $2
	`, opts.StreamID, opts.Position.Encode()).
		Scan(
			&segment.RootPieceID, &segment.EncryptedKeyNonce, &segment.EncryptedKey,
			&segment.EncryptedSize, &segment.PlainOffset, &segment.PlainSize,
			redundancyScheme{&segment.Redundancy},
			&segment.InlineData, &segment.Pieces,
		)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Segment{}, Error.New("segment missing")
		}
		return Segment{}, Error.New("unable to query segment: %w", err)
	}

	segment.StreamID = opts.StreamID
	segment.Position = opts.Position

	return segment, nil
}

// GetLatestObjectLastSegment contains arguments necessary for fetching a last segment information.
type GetLatestObjectLastSegment struct {
	ObjectLocation
}

// GetLatestObjectLastSegment returns an object last segment information.
func (db *DB) GetLatestObjectLastSegment(ctx context.Context, opts GetLatestObjectLastSegment) (segment Segment, err error) {
	defer mon.Task()(&ctx)(&err)

	if err := opts.Verify(); err != nil {
		return Segment{}, err
	}

	err = db.db.QueryRow(ctx, `
		SELECT
			stream_id, position,
			root_piece_id, encrypted_key_nonce, encrypted_key,
			encrypted_size, plain_offset, plain_size,
			redundancy,
			inline_data, remote_pieces
		FROM segments
		WHERE
			stream_id = (SELECT stream_id FROM objects WHERE
				project_id   = $1 AND
				bucket_name  = $2 AND
				object_key   = $3 AND
				status       = `+committedStatus+`
				ORDER BY version DESC
				LIMIT 1
			)
		ORDER BY position DESC
		LIMIT 1
	`, opts.ProjectID, opts.BucketName, []byte(opts.ObjectKey)).
		Scan(
			&segment.StreamID, &segment.Position,
			&segment.RootPieceID, &segment.EncryptedKeyNonce, &segment.EncryptedKey,
			&segment.EncryptedSize, &segment.PlainOffset, &segment.PlainSize,
			redundancyScheme{&segment.Redundancy},
			&segment.InlineData, &segment.Pieces,
		)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Segment{}, storj.ErrObjectNotFound.Wrap(Error.New("object or segment missing"))
		}
		return Segment{}, Error.New("unable to query segment: %w", err)
	}

	return segment, nil
}

// GetSegmentByOffset contains arguments necessary for fetching a segment information.
type GetSegmentByOffset struct {
	ObjectLocation
	PlainOffset int64
}

// GetSegmentByOffset returns an object segment information.
func (db *DB) GetSegmentByOffset(ctx context.Context, opts GetSegmentByOffset) (segment Segment, err error) {
	defer mon.Task()(&ctx)(&err)

	if err := opts.Verify(); err != nil {
		return Segment{}, err
	}

	if opts.PlainOffset < 0 {
		return Segment{}, ErrInvalidRequest.New("Invalid PlainOffset: %d", opts.PlainOffset)
	}

	err = db.db.QueryRow(ctx, `
		SELECT
			stream_id, position,
			root_piece_id, encrypted_key_nonce, encrypted_key,
			encrypted_size, plain_offset, plain_size,
			redundancy,
			inline_data, remote_pieces
		FROM segments
		WHERE
			stream_id = (SELECT stream_id FROM objects WHERE
				project_id   = $1 AND
				bucket_name  = $2 AND
				object_key   = $3 AND
				status       = `+committedStatus+`
				ORDER BY version DESC
				LIMIT 1
			) AND
			plain_offset <= $4 AND
			(plain_size + plain_offset) > $4
		ORDER BY plain_offset ASC
		LIMIT 1
	`, opts.ProjectID, opts.BucketName, []byte(opts.ObjectKey), opts.PlainOffset).
		Scan(
			&segment.StreamID, &segment.Position,
			&segment.RootPieceID, &segment.EncryptedKeyNonce, &segment.EncryptedKey,
			&segment.EncryptedSize, &segment.PlainOffset, &segment.PlainSize,
			redundancyScheme{&segment.Redundancy},
			&segment.InlineData, &segment.Pieces,
		)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Segment{}, storj.ErrObjectNotFound.Wrap(Error.New("object or segment missing"))
		}
		return Segment{}, Error.New("unable to query segment: %w", err)
	}

	return segment, nil
}

// BucketEmpty contains arguments necessary for checking if bucket is empty.
type BucketEmpty struct {
	ProjectID  uuid.UUID
	BucketName string
}

// BucketEmpty returns true if bucket does not contain objects (pending or committed).
// This method doesn't check bucket existence.
func (db *DB) BucketEmpty(ctx context.Context, opts BucketEmpty) (empty bool, err error) {
	defer mon.Task()(&ctx)(&err)

	switch {
	case opts.ProjectID.IsZero():
		return false, ErrInvalidRequest.New("ProjectID missing")
	case opts.BucketName == "":
		return false, ErrInvalidRequest.New("BucketName missing")
	}

	var value int
	err = db.db.QueryRow(ctx, `
		SELECT
			1
		FROM objects
		WHERE
			project_id   = $1 AND
			bucket_name  = $2
		LIMIT 1
	`, opts.ProjectID, opts.BucketName).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		return false, Error.New("unable to query objects: %w", err)
	}

	return false, nil
}
