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

package manager

import (
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"

	uploadClient "github.com/webx-top/client/upload"
	_ "github.com/webx-top/client/upload/driver"
	"github.com/webx-top/echo"
	"github.com/webx-top/echo/middleware/tplfunc"

	"github.com/admpub/nging/application/handler"
	"github.com/admpub/nging/application/handler/manager/file"
	"github.com/admpub/nging/application/library/common"
	modelFile "github.com/admpub/nging/application/model/file"
	"github.com/admpub/nging/application/model/file/storer"
	"github.com/admpub/nging/application/registry/upload"
	uploadPrepare "github.com/admpub/nging/application/registry/upload/prepare"
	"github.com/admpub/qrcode"
)

var (
	File                = file.File
	GetWatermarkOptions = storer.GetWatermarkOptions
	CropOptions         = modelFile.ImageOptions
)

// 文件上传保存路径规则：
// 子文件夹/表行ID/文件名

// ResponseDataForUpload 根据不同的上传方式响应不同的数据格式
func ResponseDataForUpload(ctx echo.Context, field string, err error, imageURLs []string) (result echo.H, embed bool) {
	return upload.ResponserGet(field)(ctx, field, err, imageURLs)
}

func StorerEngine() storer.Info {
	return storer.Get()
}

// SaveFilename SaveFilename(`0/`,``,`img.jpg`)
func SaveFilename(subdir, name, postFilename string) (string, error) {
	ext := filepath.Ext(postFilename)
	fname := name
	if len(fname) == 0 {
		var err error
		fname, err = common.UniqueID()
		if err != nil {
			return ``, err
		}
	}
	fname += ext
	return subdir + fname, nil
}

// Upload 上传文件
func Upload(ctx echo.Context) error {
	ownerType := `user`
	user := handler.User(ctx)
	var ownerID uint64
	if user != nil {
		ownerID = uint64(user.Id)
	}
	if ownerID < 1 {
		ctx.Data().SetError(ctx.E(`请先登录`))
		return ctx.Redirect(handler.URLFor(`/login`))
	}
	return UploadByOwner(ctx, ownerType, ownerID)
}

// UploadByOwner 上传文件
func UploadByOwner(ctx echo.Context, ownerType string, ownerID uint64) error {
	uploadType := ctx.Param(`type`)
	field := ctx.Query(`field`) // 上传表单file输入框名称
	pipe := ctx.Form(`pipe`)
	var (
		err      error
		fileURLs []string
	)
	if len(uploadType) == 0 {
		err = ctx.E(`请提供参数“%s”`, ctx.Path())
		datax, embed := ResponseDataForUpload(ctx, field, err, fileURLs)
		if !embed {
			return ctx.JSON(datax)
		}
		return err
	}
	fileType := ctx.Form(`filetype`)
	storerInfo := StorerEngine()
	prepareData, err := uploadPrepare.Prepare(ctx, uploadType, fileType, storerInfo)
	if err != nil {
		datax, embed := ResponseDataForUpload(ctx, field, err, fileURLs)
		if !embed {
			return ctx.JSON(datax)
		}
		return err
	}
	storer, err := prepareData.Storer(ctx)
	if err != nil {
		datax, embed := ResponseDataForUpload(ctx, field, err, fileURLs)
		if !embed {
			return ctx.JSON(datax)
		}
		return err
	}
	defer prepareData.Close()
	fileM := modelFile.NewFile(ctx)
	fileM.StorerName = storerInfo.Name
	fileM.StorerId = storerInfo.ID
	fileM.TableId = ``
	fileM.SetFieldName(prepareData.FieldName)
	fileM.SetTableName(prepareData.TableName)
	fileM.OwnerId = ownerID
	fileM.OwnerType = ownerType
	fileM.Type = fileType

	subdir, name, err := prepareData.Checkin(ctx, fileM)
	if err != nil {
		datax, embed := ResponseDataForUpload(ctx, field, err, fileURLs)
		if !embed {
			return ctx.JSON(datax)
		}
		return err
	}

	callback := func(result *uploadClient.Result, originalReader io.Reader, _ io.Reader) error {
		fileM.Id = 0
		fileM.SetByUploadResult(result)
		if err := ctx.Begin(); err != nil {
			return err
		}
		fileM.Use(common.Tx(ctx))
		err := prepareData.DBSaver(fileM, result, originalReader)
		if err != nil {
			ctx.Rollback()
			return err
		}
		if result.FileType.String() != `image` {
			ctx.Commit()
			return nil
		}
		thumbSizes := prepareData.AutoCropThumbSize()
		thumbM := modelFile.NewThumb(ctx)
		thumbM.CPAFrom(fileM.NgingFile)
		for _, thumbSize := range thumbSizes {
			thumbM.Reset()
			if seek, ok := originalReader.(io.Seeker); ok {
				seek.Seek(0, 0)
			}
			thumbURL := tplfunc.AddSuffix(result.FileURL, fmt.Sprintf(`_%v_%v`, thumbSize.Width, thumbSize.Height))
			cropOpt := &modelFile.CropOptions{
				Options:          CropOptions(thumbSize.Width, thumbSize.Height),
				File:             fileM.NgingFile,
				SrcReader:        originalReader,
				Storer:           storer,
				DestFile:         storer.URLToFile(thumbURL),
				FileMD5:          ``,
				WatermarkOptions: GetWatermarkOptions(),
			}
			err = thumbM.Crop(cropOpt)
			if err != nil {
				ctx.Rollback()
				return err
			}
		}
		ctx.Commit()
		return nil
	}

	clientName := ctx.Form(`client`)
	if len(clientName) > 0 {
		result := &uploadClient.Result{}
		result.SetFileNameGenerator(func(filename string) (string, error) {
			return SaveFilename(subdir, name, filename)
		})

		client := uploadClient.Upload(
			ctx,
			uploadClient.OptClientName(clientName),
			uploadClient.OptResult(result),
			uploadClient.OptStorer(storer),
			uploadClient.OptWatermarkOptions(GetWatermarkOptions()),
			uploadClient.OptChecker(prepareData.Checker),
			uploadClient.OptCallback(callback),
		)
		if client.GetError() != nil {
			if client.GetError() == upload.ErrExistsFile {
				client.SetError(nil)
			}
			return client.Response()
		}
		/*
			var reader io.ReadCloser
			reader, err = storer.Get(result.SavePath)
			if reader != nil {
				defer reader.Close()
			}
			if err != nil {
				return client.SetError(err).Response()
			}
			err = callback(result, reader, nil)
		*/
		return client.SetError(err).Response()
	}
	var results uploadClient.Results
	results, err = upload.BatchUpload(
		ctx,
		`files[]`,
		func(r *uploadClient.Result) (string, error) {
			if err := prepareData.Checker(r); err != nil {
				return ``, err
			}
			return SaveFilename(subdir, name, r.FileName)
		},
		storer,
		callback,
		GetWatermarkOptions(),
	)
	datax, embed := ResponseDataForUpload(ctx, field, err, results.FileURLs())
	if err != nil {
		if !embed {
			return ctx.JSON(datax)
		}
		return err
	}

	if pipe == `deqr` { //解析二维码
		if len(results) > 0 {
			reader, err := storer.Get(results[0].SavePath)
			if reader != nil {
				defer reader.Close()
			}
			if err != nil {
				if !embed {
					datax[`raw`] = err.Error()
					return ctx.JSON(datax)
				}
				return err
			}
			raw, err := qrcode.Decode(reader, strings.TrimPrefix(path.Ext(results[0].SavePath), `.`))
			if err != nil {
				raw = err.Error()
			}
			datax[`raw`] = raw
		}
	}
	if !embed {
		return ctx.JSON(datax)
	}
	data := ctx.Data()
	data.SetData(datax)
	return ctx.JSON(data)
}
