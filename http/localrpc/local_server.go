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

// Package localrpc privides a function to start local rpc server
package localrpc

import (
	"net/http"
	"strconv"

	"fmt"
	cfg "github.com/TesraSupernet/Tesra/common/config"
	"github.com/TesraSupernet/Tesra/common/log"
	"github.com/TesraSupernet/Tesra/http/base/rpc"
)

const (
	LOCAL_HOST string = "127.0.0.1"
	LOCAL_DIR  string = "/local"
)

func StartLocalServer() error {
	log.Debug()
	http.HandleFunc(LOCAL_DIR, rpc.Handle)

	rpc.HandleFunc("getneighbor", rpc.GetNeighbor)
	rpc.HandleFunc("getnodestate", rpc.GetNodeState)
	rpc.HandleFunc("startconsensus", rpc.StartConsensus)
	rpc.HandleFunc("stopconsensus", rpc.StopConsensus)
	rpc.HandleFunc("setdebuginfo", rpc.SetDebugInfo)

	// TODO: only listen to local host
	err := http.ListenAndServe(LOCAL_HOST+":"+strconv.Itoa(int(cfg.DefConfig.Rpc.HttpLocalPort)), nil)
	if err != nil {
		return fmt.Errorf("ListenAndServe error:%s", err)
	}
	return nil
}
