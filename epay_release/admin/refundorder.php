<?php
/**
 * 退款记录
**/
include("../includes/common.php");
$title='退款记录';
include './head.php';
if($islogin==1){}else exit("<script language='javascript'>window.location.href='./login.php';</script>");
?>
<style>
.dates{max-width: 120px;}
@media screen and (max-width: 767px) {
.table-responsive {
    overflow-y: auto;
}
}
</style>
<link href="../assets/css/datepicker.css" rel="stylesheet">
  <div class="container-fluid" style="padding-top:70px;">
    <div class="col-md-12 center-block" style="float: none;">

<form onsubmit="return searchSubmit()" method="GET" class="form-inline" id="searchToolbar">
  <div class="form-group">
    <label>搜索</label>
	<select name="column" class="form-control"><option value="refund_no">退款单号</option><option value="out_refund_no">商户退款单号</option><option value="trade_no">关联订单号</option></select>
  </div>
  <div class="form-group">
    <input type="text" class="form-control" name="value" placeholder="搜索内容" value="">
  </div>
  <div class="form-group">
    <input type="text" class="form-control" name="uid" style="width: 100px;" placeholder="商户号" value="">
  </div>
  <div class="input-group input-daterange">
	<input type="text" id="starttime" name="starttime" class="form-control dates" placeholder="开始日期" autocomplete="off" title="留空则不限时间范围">
	<span class="input-group-addon" onclick="$('#starttime').val('');$('#endtime').val('');" title="清除"><i class="fa fa-chevron-right"></i></span>
	<input type="text" id="endtime" name="endtime" class="form-control dates" placeholder="结束日期" autocomplete="off" title="留空则不限时间范围">
  </div>
  <div class="form-group">
	<select name="status" class="form-control"><option value="">全部状态</option><option value="0">退款中</option><option value="1">退款成功</option><option value="2">退款失败</option></select>
  </div>
  <button type="submit" class="btn btn-primary">&nbsp;搜索&nbsp;</button>
  <a href="javascript:searchClear()" class="btn btn-default" title="刷新退款记录"><i class="fa fa-refresh"></i></a>
  <button type="button" onclick="statistics()" class="btn btn-default">&nbsp;统计&nbsp;</button>
</form>
      <table id="listTable">
	  </table>
    </div>
  </div>
<div class="modal" id="modal-statistics" role="dialog" aria-labelledby="myModalLabel" aria-hidden="true" data-backdrop="static">
	<div class="modal-dialog">
		<div class="modal-content animated flipInX">
			<div class="modal-header">
				<button type="button" class="close" data-dismiss="modal"><span aria-hidden="true">&times;</span><span class="sr-only">Close</span></button>
				<h4 class="modal-title">退款记录统计</h4>
			</div>
			<div class="modal-body">
			</div>
			<div class="modal-footer">
				<button type="button" class="btn btn-primary" data-dismiss="modal">关闭</button>
			</div>
		</div>
	</div>
</div>
<template id="statistics-template">
<ul class="list-inline" style="margin-bottom: 0;padding-bottom: 10px;border-bottom: 1px solid #dddddd;">
    <li>退款总笔数：<span style="font-weight: 600;">{totalCount}</span></li>
    <li>退款中：<span style="font-weight: 600;">{processingCount}</span></li>
    <li>退款成功：<span style="font-weight: 600;">{successCount}</span></li>
    <li>退款失败：<span style="font-weight: 600;">{failCount}</span></li>
</ul>
<ul class="list-inline" style="padding-top:10px;margin-bottom: 0;">
    <li>退款总金额：<span style="font-weight: 600;">¥ {totalMoney}</span></li>
    <li>退款中金额：<span style="font-weight: 600;">¥ {processingMoney}</span></li>
    <li>退款成功金额：<span style="font-weight: 600;">¥ {successMoney}</span></li>
    <li>退款失败金额：<span style="font-weight: 600;">¥ {failMoney}</span></li>
</ul>
</template>
<script src="<?php echo $cdnpublic?>layer/3.1.1/layer.js"></script>
<script src="<?php echo $cdnpublic?>bootstrap-datepicker/1.10.0/js/bootstrap-datepicker.min.js"></script>
<script src="<?php echo $cdnpublic?>bootstrap-datepicker/1.10.0/locales/bootstrap-datepicker.zh-CN.min.js"></script>
<script src="../assets/js/bootstrap-table.min.js"></script>
<script src="../assets/js/bootstrap-table-page-jump-to.min.js"></script>
<script src="../assets/js/custom.js"></script>
<script>
$(document).ready(function(){
	updateToolbar();
	const defaultPageSize = 30;
	const pageNumber = typeof window.$_GET['pageNumber'] != 'undefined' ? parseInt(window.$_GET['pageNumber']) : 1;
	const pageSize = typeof window.$_GET['pageSize'] != 'undefined' ? parseInt(window.$_GET['pageSize']) : defaultPageSize;

	$("#listTable").bootstrapTable({
		url: 'ajax_order.php?act=refundOrderList',
		pageNumber: pageNumber,
		pageSize: pageSize,
		classes: 'table table-striped table-hover table-bordered',
		uniqueId: 'refund_no',
		columns: [
			{
				field: 'refund_no',
				title: '退款单号<br/>商户退款单号',
				formatter: function(value, row, index) {
					return value+'<br/>'+(row.out_refund_no ? row.out_refund_no : '&nbsp;');
				}
			},
			{
				field: 'trade_no',
				title: '关联订单号',
				formatter: function(value, row, index) {
					return '<a href="./order.php?column=trade_no&value='+encodeURIComponent(value)+'" target="_blank">'+value+'</a>';
				}
			},
			{
				field: 'uid',
				title: '商户号',
				formatter: function(value, row, index) {
					return '<a href="./ulist.php?my=search&column=uid&value='+encodeURIComponent(value)+'" target="_blank">'+value+'</a>';
				}
			},
			{
				field: 'money',
				title: '退款金额<br/>扣减金额',
				formatter: function(value, row, index) {
					return '¥<b>'+value+'</b><br/>¥<b>'+(row.reducemoney ? row.reducemoney : '0.00')+'</b>';
				}
			},
			{
				field: 'addtime',
				title: '申请时间<br/>完成时间',
				formatter: function(value, row, index) {
					return (value ? value : '&nbsp;')+'<br/>'+(row.endtime ? row.endtime : '&nbsp;');
				}
			},
			{
				field: 'status',
				title: '状态',
				formatter: function(value, row, index) {
					if(value == '1'){
						return '<font color="green">退款成功</font>';
					}else if(value == '2'){
						return '<font color="red">退款失败</font>';
					}else{
						return '<font color="blue">退款中</font>';
					}
				}
			},
		],
	})
	$('.input-datepicker, .input-daterange').datepicker({
        format: 'yyyy-mm-dd',
		autoclose: true,
        clearBtn: true,
        language: 'zh-CN'
    });
})
function statistics(){
    var ii = layer.load(2, {shade:[0.1,'#fff']});
    $.ajax({
        type : 'POST',
        url : 'ajax_order.php?act=refundOrderStats',
        data: $('#searchToolbar').serializeArray(),
        dataType : 'json',
        success : function(data) {
            layer.close(ii);
            if(data.code == 0){
                var element = $('#modal-statistics');
                var htmlContent = $("#statistics-template").html().replace(/\{(\w+)\}/g, function (match, key) {
                    return data.data[key] || '0';
                });
                element.find('.modal-body').html(htmlContent);
                element.modal('show');
            }else{
                layer.alert(data.msg);
            }
        },
        error:function(data){
            layer.close(ii);
            layer.msg('服务器错误');
        }
    });
}
</script>
