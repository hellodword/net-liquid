/*
Copyright (C) BABEC. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package qtls

import (
	"crypto/x509"
	"fmt"
	"sync"

	"chainmaker.org/chainmaker/chainmaker-net-common/common"
	"chainmaker.org/chainmaker/chainmaker-net-libp2p/utils"
	cmx509 "chainmaker.org/chainmaker/common/crypto/x509"
	"github.com/libp2p/go-libp2p-core/peer"
	qx509 "github.com/xiaotianfork/q-tls-common/x509"
)

// DerivedInfoWithCert contains infos loaded from tls cert when verifying peer certificate.
type DerivedInfoWithCert struct {
	TlsCertBytes []byte
	ChainIds     []string
	PeerId       string
	CertId       string
}

// CertValidator wraps a ChainTrustRoots instance and a common.RevokedValidator.
// It provides a function for verifying peer certificate when tls handshaking.
// In handshaking process, the function will load remote tls certificate and verify it by chain trust roots, also load remote peer id and cert id. All these infos will stored in validator.
// These infos could be queried with QueryDerivedInfoWithPeerId function, and could be removed with CleanDerivedInfoWithPeerId function.
type CertValidator struct {
	tlsTrustRoots    *ChainTrustRoots
	revokedValidator *common.RevokedValidator
	infoStore        map[string]*DerivedInfoWithCert // map[peer.ID]*DerivedInfoWithCert
	mu               sync.RWMutex
}

// NewCertValidator create a new CertValidator instance.
func NewCertValidator(tlsTrustRoots *ChainTrustRoots, revokedValidator *common.RevokedValidator) *CertValidator {
	return &CertValidator{
		tlsTrustRoots:    tlsTrustRoots,
		revokedValidator: revokedValidator,
		infoStore:        make(map[string]*DerivedInfoWithCert),
		mu:               sync.RWMutex{},
	}
}

// VerifyPeerCertificateFunc provides a function for verify peer certificate in tls config.
// In handshaking process, the function will load remote tls certificate and verify it by chain trust roots,
// also load remote peer id and cert id. All these infos will stored in validator.
func (v *CertValidator) VerifyPeerCertificateFunc() func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		revoked, err := isRevoked(v.revokedValidator, rawCerts)
		if err != nil {
			return err
		}
		if revoked {
			return fmt.Errorf("certificate revoked")
		}
		tlsCertBytes := rawCerts[0]
		cert, err := qx509.ParseCertificate(tlsCertBytes)
		if err != nil {
			return fmt.Errorf("parse certificate failed: %s", err.Error())
		}
		chainIds, err := v.tlsTrustRoots.VerifyCert(cert)
		if err != nil {
			return fmt.Errorf("verify certificate failed: %s", err.Error())
		}
		pubKey, err := utils.ParsePublicKeyToPubKey4Quic(cert.PublicKey)
		if err != nil {
			return fmt.Errorf("parse pubkey failed: %s", err.Error())
		}
		pid, err := peer.IDFromPublicKey(pubKey)
		if err != nil {
			return fmt.Errorf("parse pid from pubkey failed: %s", err.Error())
		}
		peerId := pid.Pretty()
		certId, err := cmx509.GetNodeIdFromCertificate(cmx509.OidNodeId, *qx509.ToStandX509Cert(cert))
		if err != nil {
			return fmt.Errorf("get certid failed: %s", err.Error())
		}

		info := &DerivedInfoWithCert{
			TlsCertBytes: tlsCertBytes,
			ChainIds:     chainIds,
			PeerId:       peerId,
			CertId:       string(certId),
		}

		v.mu.Lock()
		defer v.mu.Unlock()
		v.infoStore[peerId] = info
		return nil
	}
}

// QueryDerivedInfoWithPeerId return all infos that loaded with VerifyPeerCertificateFunc and stored in validator.
func (v *CertValidator) QueryDerivedInfoWithPeerId(peerId string) *DerivedInfoWithCert {
	v.mu.RLock()
	defer v.mu.RUnlock()
	res, ok := v.infoStore[peerId]
	if !ok {
		return nil
	}
	return res
}

func isRevoked(revokeValidator *common.RevokedValidator, rawCerts [][]byte) (bool, error) {
	certs := make([]*cmx509.Certificate, 0)
	for idx := range rawCerts {
		cert, err := cmx509.ParseCertificate(rawCerts[idx])
		if err != nil {
			return false, err
		}
		certs = append(certs, cert)
	}
	return revokeValidator.ValidateCertsIsRevoked(certs), nil
}
