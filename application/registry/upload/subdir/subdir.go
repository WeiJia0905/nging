/*
   Nging is a toolbox for webmasters
   Copyright (C) 2018-present  Wenhui Shen <swh@admpub.com>

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published
   by the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package subdir

import (
	"fmt"
	"strings"

	"github.com/admpub/color"
	"github.com/admpub/log"
	"github.com/admpub/nging/application/registry/upload/checker"
)

var (
	subdirs   = map[string]*SubdirInfo{}
	table2dir = map[string]string{}
)

func init() {
	//后台用户文件
	RegisterObject((&SubdirInfo{
		Allowed:     true,
		Key:         "nging_user",
		Name:        "后台用户",
		Description: "",
	}).SetTableName("nging_user").
		SetFieldName(`:个人文件`, `avatar:头像`))

	//后台系统设置中的图片
	RegisterObject((&SubdirInfo{
		Allowed:     true,
		Key:         "nging_config",
		Name:        "站点配置",
		Description: "",
		checker:     checker.ConfigChecker,
	}).SetTableName("nging_config")).
		SetFieldName(`:内容图片`)
}

func Register(subdir interface{}, nameAndDescription ...string) *SubdirInfo {
	var key string
	switch v := subdir.(type) {
	case string:
		key = v
	case *SubdirInfo:
		return RegisterObject(v)
	case SubdirInfo:
		return RegisterObject(&v)
	default:
		panic(fmt.Sprintf(`Unsupported type: %T`, v))
	}
	var name, nameEN, description string
	switch len(nameAndDescription) {
	case 3:
		description = nameAndDescription[2]
		fallthrough
	case 2:
		nameEN = nameAndDescription[1]
		fallthrough
	case 1:
		name = nameAndDescription[0]
	}
	info := &SubdirInfo{
		Allowed:     true,
		Key:         key,
		Name:        name,
		NameEN:      nameEN,
		Description: description,
	}

	r := strings.SplitN(info.Key, `.`, 2)
	switch len(r) {
	case 2:
		info.SetFieldName(r[1])
		fallthrough
	case 1:
		info.tableName = r[0]
	}
	RegisterObject(info)
	return info
}

func RegisterObject(info *SubdirInfo) *SubdirInfo {
	in, ok := subdirs[info.Key]
	if ok {
		return in.CopyFrom(info)
	}
	subdirs[info.Key] = info
	log.Info(color.MagentaString(`subdir.register:`), info.Key)
	return info
}

func Unregister(subdirList ...string) {
	for _, subdir := range subdirList {
		_, ok := subdirs[subdir]
		if ok {
			delete(subdirs, subdir)
		}
	}
}

func All() map[string]*SubdirInfo {
	return subdirs
}

func IsAllowed(subdir string, defaults ...string) bool {
	info, ok := subdirs[subdir]
	if !ok || info == nil {
		if len(defaults) > 0 && defaults[0] != subdir {
			return IsAllowed(defaults[0])
		}
		return false
	}
	return info.Allowed
}

func Get(subdir string) *SubdirInfo {
	info, _ := subdirs[subdir]
	return info
}

func GetOrCreate(subdir string) *SubdirInfo {
	info, ok := subdirs[subdir]
	if !ok {
		return RegisterObject(NewSubdirInfo(subdir, ``))
	}
	return info
}

func GetByTable(table string) *SubdirInfo {
	subdir, ok := table2dir[table]
	if !ok {
		return nil
	}
	return Get(subdir)
}

// CleanTempFile 清理临时文件
func CleanTempFile(prefix string, deleter func(folderPath string) error) error {
	if !strings.HasSuffix(prefix, `/`) {
		prefix += `/`
	}
	for subdir := range subdirs {
		err := deleter(prefix + subdir + `/0/`)
		if err != nil {
			return err
		}
	}
	return nil
}
