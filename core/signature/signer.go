/*
 * Copyright (C) 2019 The TesraSupernet Authors
 * This file is part of The TesraSupernet library.
 *
 * The TesraSupernet is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The TesraSupernet is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Lesser General Public License for more details.
 *
 * You should have received a copy of the GNU Lesser General Public License
 * along with The TesraSupernet.  If not, see <http://www.gnu.org/licenses/>.
 */

package signature

import (
	"github.com/TesraSupernet/tesracrypto/keypair"
	"github.com/TesraSupernet/tesracrypto/signature"
)

// Signer is the abstract interface of user's information(Keys) for signing data.
type Signer interface {
	//get signer's private key
	PrivKey() keypair.PrivateKey

	//get signer's public key
	PubKey() keypair.PublicKey

	Scheme() signature.SignatureScheme
}
